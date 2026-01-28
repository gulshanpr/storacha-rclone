// Storacha upload client using Node.js worker pool
package storacha

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/storacha/go-ucanto/principal/ed25519/signer"

	"github.com/gulshanpr/rclone/internal/config"
)

func GetDIDFromPrivateKey(privateKey string) (string, error) {
	s, err := signer.Parse(privateKey)
	if err != nil {
		return "", fmt.Errorf("parse private key: %w", err)
	}
	return s.DID().String(), nil
}

type workerRequest struct {
	Action   string `json:"action"`
	SpaceDID string `json:"spaceDID,omitempty"`
	Path     string `json:"path,omitempty"`
}

type workerResponse struct {
	Success bool   `json:"success"`
	CID     string `json:"cid,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

type Worker struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   *bufio.Reader
	mu       sync.Mutex
	alive    bool
	spaceDID string
}

type WorkerPool struct {
	workers   []*Worker
	available chan *Worker
	size      int
	spaceDID  string
	mu        sync.Mutex
	closed    bool
}

type Client struct {
	pool *WorkerPool
}

func NewClient(cfg config.StorachaConfig) (*Client, error) {
	// Verify node is installed
	if _, err := exec.LookPath("node"); err != nil {
		return nil, fmt.Errorf("node not found. Install Node.js from: https://nodejs.org")
	}

	// Verify storacha CLI is installed
	if _, err := exec.LookPath("storacha"); err != nil {
		return nil, fmt.Errorf("storacha CLI not found. Install with: npm install -g @storacha/cli")
	}

	// Create worker pool
	pool, err := newWorkerPool(cfg.SpaceDID, 3) // 3 workers by default
	if err != nil {
		return nil, fmt.Errorf("create worker pool: %w", err)
	}

	return &Client{
		pool: pool,
	}, nil
}

func (c *Client) UploadFile(ctx context.Context, filePath string) (string, error) {
	// Get absolute path
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("get absolute path: %w", err)
	}

	// Check file exists
	if _, err := os.Stat(absPath); err != nil {
		return "", fmt.Errorf("file not found: %w", err)
	}

	fmt.Println("Uploading to Storacha network...")

	// Get worker from pool
	worker, err := c.pool.getWorker(ctx)
	if err != nil {
		return "", fmt.Errorf("get worker: %w", err)
	}
	defer c.pool.putWorker(worker)

	// Upload using worker
	cid, err := worker.upload(ctx, absPath)
	if err != nil {
		// If worker failed, mark it as dead and try to get a new one
		worker.markDead()
		return "", fmt.Errorf("upload failed: %w", err)
	}

	return cid, nil
}

func (c *Client) Close() error {
	return c.pool.Close()
}

func newWorkerPool(spaceDID string, size int) (*WorkerPool, error) {
	pool := &WorkerPool{
		workers:   make([]*Worker, 0, size),
		available: make(chan *Worker, size),
		size:      size,
		spaceDID:  spaceDID,
	}

	// Create workers
	for i := 0; i < size; i++ {
		worker, err := newWorker(spaceDID)
		if err != nil {
			// Clean up any created workers
			pool.Close()
			return nil, fmt.Errorf("create worker %d: %w", i, err)
		}
		pool.workers = append(pool.workers, worker)
		pool.available <- worker
	}

	return pool, nil
}

func (p *WorkerPool) getWorker(ctx context.Context) (*Worker, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("worker pool closed")
	}
	p.mu.Unlock()

	select {
	case worker := <-p.available:
		if !worker.isAlive() {
			// Try to restart dead worker
			if err := worker.restart(); err != nil {
				// Return to pool and try to get another
				p.available <- worker
				return p.getWorker(ctx)
			}
		}
		return worker, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *WorkerPool) putWorker(worker *Worker) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.closed {
		select {
		case p.available <- worker:
		default:
			// Should never happen
		}
	}
}

func (p *WorkerPool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	var lastErr error
	for _, worker := range p.workers {
		if err := worker.shutdown(); err != nil {
			lastErr = err
		}
	}
	close(p.available)
	return lastErr
}

func newWorker(spaceDID string) (*Worker, error) {
	// path to worker.js
	workerScript := filepath.Join(getPackageDir(), "worker.js")

	// Check if worker.js exists
	if _, err := os.Stat(workerScript); err != nil {
		return nil, fmt.Errorf("worker.js not found at %s: %w", workerScript, err)
	}

	cmd := exec.Command("node", workerScript)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("start worker: %w", err)
	}

	worker := &Worker{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   bufio.NewReader(stdout),
		alive:    true,
		spaceDID: spaceDID,
	}

	// Wait for ready message
	if err := worker.waitForReady(); err != nil {
		worker.shutdown()
		return nil, fmt.Errorf("worker initialization: %w", err)
	}

	// Initialize with space
	if err := worker.initialize(); err != nil {
		worker.shutdown()
		return nil, fmt.Errorf("initialize worker: %w", err)
	}

	return worker, nil
}

func (w *Worker) waitForReady() error {
	// timeout for ready message
	done := make(chan error, 1)

	go func() {
		line, err := w.stdout.ReadString('\n')
		if err != nil {
			done <- err
			return
		}

		var resp workerResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			done <- fmt.Errorf("parse ready message: %w", err)
			return
		}

		if !resp.Success {
			done <- fmt.Errorf("worker not ready: %s", resp.Error)
			return
		}

		done <- nil
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		return fmt.Errorf("timeout waiting for worker ready")
	}
}

func (w *Worker) initialize() error {
	req := workerRequest{
		Action:   "init",
		SpaceDID: w.spaceDID,
	}

	resp, err := w.sendRequest(req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("init failed: %s", resp.Error)
	}

	return nil
}

func (w *Worker) upload(ctx context.Context, filePath string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.alive {
		return "", fmt.Errorf("worker is dead")
	}

	req := workerRequest{
		Action: "upload",
		Path:   filePath,
	}

	// Send request with timeout
	done := make(chan struct {
		resp workerResponse
		err  error
	}, 1)

	go func() {
		resp, err := w.sendRequest(req)
		done <- struct {
			resp workerResponse
			err  error
		}{resp, err}
	}()

	select {
	case result := <-done:
		if result.err != nil {
			return "", result.err
		}
		if !result.resp.Success {
			return "", fmt.Errorf("upload failed: %s", result.resp.Error)
		}
		return result.resp.CID, nil
	case <-ctx.Done():
		w.alive = false
		return "", ctx.Err()
	}
}

func (w *Worker) sendRequest(req workerRequest) (workerResponse, error) {
	var resp workerResponse

	// Send request
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return resp, fmt.Errorf("marshal request: %w", err)
	}

	if _, err := fmt.Fprintf(w.stdin, "%s\n", reqJSON); err != nil {
		w.alive = false
		return resp, fmt.Errorf("write request: %w", err)
	}

	// Read response
	line, err := w.stdout.ReadString('\n')
	if err != nil {
		w.alive = false
		return resp, fmt.Errorf("read response: %w", err)
	}

	// Run upload using storacha up(js-client)
	fmt.Println("Uploading to Storacha network...")
	var stdout, stderr bytes.Buffer
	uploadCmd := exec.CommandContext(ctx, "storacha", "up", absPath)
	uploadCmd.Stdout = &stdout
	uploadCmd.Stderr = &stderr
	if err := uploadCmd.Run(); err != nil {
		return "", fmt.Errorf("storacha up failed: %w\nstderr: %s", err, stderr.String())
	}

	output := stdout.String()
	cid := extractCID(output)

	if cid == "" {
		cid = extractCID(stderr.String())
	}

	if cid == "" {
		return "", fmt.Errorf("could not extract CID from output:\nstdout: %s\nstderr: %s", output, stderr.String())
	}

	return cid, nil
}

func extractCID(output string) string {
	re := regexp.MustCompile(`(bafy[a-zA-Z0-9]{50,})`)
	matches := re.FindStringSubmatch(output)
	if len(matches) > 1 {
		return matches[1]
	}

	re2 := regexp.MustCompile(`(bafk[a-zA-Z0-9]{50,})`)
	matches2 := re2.FindStringSubmatch(output)
	if len(matches2) > 1 {
		return matches2[1]
	}

	re3 := regexp.MustCompile(`ipfs/(bafy[a-zA-Z0-9]+|bafk[a-zA-Z0-9]+)`)
	matches3 := re3.FindStringSubmatch(output)
	if len(matches3) > 1 {
		return matches3[1]
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "bafy") || strings.HasPrefix(line, "bafk") {
			return line
		}
	}

	return ""
}

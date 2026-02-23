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
	"runtime"
	"strings"
	"sync"
	"time"
	"net/http"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

	fmt.Println("Setting space...")
	useCmd := exec.CommandContext(ctx, "storacha", "space", "use", c.pool.spaceDID)
	useCmd.Stderr = os.Stderr
	if err := useCmd.Run(); err != nil {
		return "", fmt.Errorf("storacha space use: %w", err)
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

func (c *Client) UploadFromS3(ctx context.Context, awsCfg config.AppConfig, s3Key string) (string, error) {
	// Set space
	fmt.Println("Setting space...")
	useCmd := exec.CommandContext(ctx, "storacha", "space", "use", c.pool.spaceDID)
	useCmd.Stderr = os.Stderr
	if err := useCmd.Run(); err != nil {
		return "", fmt.Errorf("storacha space use: %w", err)
	}
	fmt.Println("✓ Space configured")

	// Configure AWS
	fmt.Println("Configuring AWS client...")
	creds := credentials.NewStaticCredentialsProvider(awsCfg.AccessKeyID, awsCfg.SecretAccessKey, "")
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(awsCfg.Region),
		awsconfig.WithCredentialsProvider(creds),
	)
	if err != nil {
		return "", fmt.Errorf("aws config: %w", err)
	}

	// Get S3 object - stream directly to storacha stdin
	fmt.Println("Streaming from S3 to Storacha...")
	s3Client := s3.NewFromConfig(cfg)
	resp, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: awssdk.String(awsCfg.Bucket),
		Key:    awssdk.String(s3Key),
	})
	if err != nil {
		return "", fmt.Errorf("s3 get object: %w", err)
	}
	defer resp.Body.Close()

	// Start storacha upload command with stdin
	var stdout, stderr bytes.Buffer
	uploadCmd := exec.CommandContext(ctx, "storacha", "up", "--no-wrap", "-")
	uploadCmd.Stdin = resp.Body
	uploadCmd.Stdout = &stdout
	uploadCmd.Stderr = &stderr

	fmt.Println("Starting upload...")
	if err := uploadCmd.Run(); err != nil {
		return "", fmt.Errorf("storacha up failed: %w\nstderr: %s", err, stderr.String())
	}

	fmt.Println("✓ Upload completed")

	// Extract CID
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

func (c *Client) DownloadToReader(ctx context.Context, cid string, fileName string) (io.ReadCloser, int64, error) {
	var url string
	if fileName != "" {
		url = fmt.Sprintf("https://%s.ipfs.w3s.link/%s", cid, fileName)
	} else {
		url = fmt.Sprintf("https://%s.ipfs.w3s.link", cid)
	}

	fmt.Printf("Fetching from: %s\n", url)

	httpClient := &http.Client{
		Timeout: 10 * time.Minute,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("http get: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("gateway returned status: %s", resp.Status)
	}

	return resp.Body, resp.ContentLength, nil
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

	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return resp, fmt.Errorf("parse response: %w", err)
	}

	return resp, nil
}

func (w *Worker) isAlive() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.alive
}

func (w *Worker) markDead() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.alive = false
}

func (w *Worker) restart() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Kill old process
	if w.cmd != nil && w.cmd.Process != nil {
		w.cmd.Process.Kill()
	}
	w.stdin.Close()

	// Create new worker
	newWorker, err := newWorker(w.spaceDID)
	if err != nil {
		return err
	}

	// Replace internals
	w.cmd = newWorker.cmd
	w.stdin = newWorker.stdin
	w.stdout = newWorker.stdout
	w.alive = true

	return nil
}

func (w *Worker) shutdown() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.alive {
		return nil
	}

	req := workerRequest{Action: "shutdown"}
	reqJSON, _ := json.Marshal(req)
	fmt.Fprintf(w.stdin, "%s\n", reqJSON)

	done := make(chan error, 1)
	go func() {
		done <- w.cmd.Wait()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		// Force kill
		if w.cmd.Process != nil {
			w.cmd.Process.Kill()
		}
	}

	w.stdin.Close()
	w.alive = false
	return nil
}

func getPackageDir() string {
	// Get the directory of the current file
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Dir(filename)
}

// Extract CID from output
func extractCID(output string) string {
	// Match CIDv1 (bafy...) or CIDv0 (Qm...)
	re := regexp.MustCompile(`\b(bafy[a-zA-Z0-9]{50,}|Qm[a-zA-Z0-9]{44,})\b`)
	for _, line := range strings.Split(output, "\n") {
		if m := re.FindString(line); m != "" {
			return m
		}
	}
	return ""
}

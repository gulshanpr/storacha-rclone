// Storacha upload client using Node.js worker pool
package storacha

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

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
	useCmd := exec.CommandContext(ctx, "storacha", "space", "use", c.spaceDID)
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

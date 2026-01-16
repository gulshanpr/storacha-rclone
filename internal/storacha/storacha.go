// Storacha upload client using Guppy CLI
package storacha

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/storacha/go-ucanto/principal/ed25519/signer"

	"github.com/gulshanpr/rclone/internal/config"
)

// GetDIDFromPrivateKey returns the DID for a given private key
func GetDIDFromPrivateKey(privateKey string) (string, error) {
	s, err := signer.Parse(privateKey)
	if err != nil {
		return "", fmt.Errorf("parse private key: %w", err)
	}
	return s.DID().String(), nil
}

type Client struct {
	spaceDID string
}

func NewClient(cfg config.StorachaConfig) (*Client, error) {
	// Verify guppy CLI is installed
	if _, err := exec.LookPath("guppy"); err != nil {
		return nil, fmt.Errorf("guppy CLI not found. Install with: go install github.com/storacha/guppy@latest")
	}

	return &Client{
		spaceDID: cfg.SpaceDID,
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

	// Add source to space
	fmt.Println("Adding source to space...")
	addCmd := exec.CommandContext(ctx, "guppy", "upload", "source", "add", c.spaceDID, absPath)
	addCmd.Stderr = os.Stderr
	if err := addCmd.Run(); err != nil {
		return "", fmt.Errorf("guppy upload source add: %w", err)
	}

	// Run upload
	fmt.Println("Uploading to Storacha network...")
	var stdout bytes.Buffer
	uploadCmd := exec.CommandContext(ctx, "guppy", "upload", c.spaceDID)
	uploadCmd.Stdout = &stdout
	uploadCmd.Stderr = os.Stderr
	if err := uploadCmd.Run(); err != nil {
		return "", fmt.Errorf("guppy upload: %w", err)
	}

	// Extract CID from output
	output := stdout.String()
	cid := extractCID(output)
	if cid == "" {
		// Try to get from the last line
		lines := strings.Split(strings.TrimSpace(output), "\n")
		if len(lines) > 0 {
			cid = strings.TrimSpace(lines[len(lines)-1])
		}
	}

	if cid == "" {
		return "", fmt.Errorf("could not extract CID from output: %s", output)
	}

	return cid, nil
}

func extractCID(output string) string {
	// Look for bafy... CID pattern
	re := regexp.MustCompile(`(bafy[a-z0-9]{50,})`)
	matches := re.FindStringSubmatch(output)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

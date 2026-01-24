// Storacha upload client using storacha CLI
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
	// Verify storacha CLI is installed
	if _, err := exec.LookPath("storacha"); err != nil {
		return nil, fmt.Errorf("storacha CLI not found. Install with: npm install -g @storacha/cli")
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

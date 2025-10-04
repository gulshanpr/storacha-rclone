package commands

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"

	"github.com/gulshanpr/rclone/internal/aws"
	"github.com/gulshanpr/rclone/internal/config"
	"golang.org/x/term"
)

func prompt(line string) (string, error) {
	fmt.Print(line)
	reader := bufio.NewReader(os.Stdin)
	s, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

func promptSecret(line string) (string, error) {
	fmt.Print(line)
	b, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func AWSLogin() {
	fmt.Println("== storacha-rclone AWS login ==")
	akid, err := prompt("AWS Access Key ID: ")
	if err != nil {
		log.Fatal(err)
	}
	secret, err := promptSecret("AWS Secret Access Key: ")
	if err != nil {
		log.Fatal(err)
	}
	region, err := prompt("Default AWS Region (e.g., us-east-1): ")
	if err != nil {
		log.Fatal(err)
	}
	bucket, err := prompt("Default S3 bucket name: ")
	if err != nil {
		log.Fatal(err)
	}
	cfg := config.AppConfig{
		AccessKeyID:     akid,
		SecretAccessKey: secret,
		Region:          region,
		Bucket:          bucket,
	}
	if err := cfg.Save(); err != nil {
		log.Fatalf("save config: %v", err)
	}
	fmt.Println("Saved. (stored in ~/.storacha-rclone/config.json with 0600 perms)")
}

func S3List(args []string) {
	fs := flag.NewFlagSet("s3-ls", flag.ExitOnError)
	prefix := fs.String("prefix", "", "prefix to filter objects (optional)")
	fs.Parse(args)

	ac, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	if err := aws.ListObjects(ctx, ac, prefix); err != nil {
		log.Fatal(err)
	}
}

func S3Get(args []string) {
	fs := flag.NewFlagSet("s3-get", flag.ExitOnError)
	key := fs.String("key", "", "object key to download (required)")
	outFile := fs.String("out", "", "local output filename (defaults to basename of key)")
	fs.Parse(args)

	if *key == "" {
		fs.Usage()
		os.Exit(2)
	}

	ac, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	if err := aws.DownloadObject(ctx, ac, *key, *outFile); err != nil {
		log.Fatal(err)
	}
}

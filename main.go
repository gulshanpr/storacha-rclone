package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/term"
)

type AppConfig struct {
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".storacha-rclone")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func saveConfig(cfg AppConfig) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// Write with 0600 perms
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func loadConfig() (AppConfig, error) {
	var cfg AppConfig
	p, err := configPath()
	if err != nil {
		return cfg, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return cfg, fmt.Errorf("read config: %w (run `storacha-rclone aws-login` first)", err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" || cfg.Region == "" || cfg.Bucket == "" {
		return cfg, fmt.Errorf("config incomplete, run `storacha-rclone aws-login` again")
	}
	return cfg, nil
}

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

func awsConfigFromLocal(ctx context.Context, ac AppConfig) (aws.Config, error) {
	creds := credentials.NewStaticCredentialsProvider(ac.AccessKeyID, ac.SecretAccessKey, "")
	return config.LoadDefaultConfig(ctx,
		config.WithRegion(ac.Region),
		config.WithCredentialsProvider(creds),
	)
}

func cmdAWSLogin() {
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
	cfg := AppConfig{
		AccessKeyID:     akid,
		SecretAccessKey: secret,
		Region:          region,
		Bucket:          bucket,
	}
	if err := saveConfig(cfg); err != nil {
		log.Fatalf("save config: %v", err)
	}
	fmt.Println("Saved. (stored in ~/.storacha-rclone/config.json with 0600 perms)")
}

func cmdS3List(args []string) {
	fs := flag.NewFlagSet("s3-ls", flag.ExitOnError)
	prefix := fs.String("prefix", "", "prefix to filter objects (optional)")
	fs.Parse(args)

	ac, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	awscfg, err := awsConfigFromLocal(ctx, ac)
	if err != nil {
		log.Fatalf("AWS config: %v", err)
	}
	client := s3.NewFromConfig(awscfg)

	var token *string
	for {
		out, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            &ac.Bucket,
			Prefix:            prefix,
			ContinuationToken: token,
		})
		if err != nil {
			log.Fatalf("ListObjectsV2: %v", err)
		}
		for _, obj := range out.Contents {
			size := obj.Size
			key := *obj.Key
			fmt.Printf("%12d  %s\n", size, key)
		}
		if *out.IsTruncated {
			token = out.NextContinuationToken // this may be nil, that's okay
			continue
		}

		break
	}
}

func cmdS3Get(args []string) {
	fs := flag.NewFlagSet("s3-get", flag.ExitOnError)
	key := fs.String("key", "", "object key to download (required)")
	outFile := fs.String("out", "", "local output filename (defaults to basename of key)")
	fs.Parse(args)

	if *key == "" {
		fs.Usage()
		os.Exit(2)
	}
	dest := *outFile
	if dest == "" {
		parts := strings.Split(*key, "/")
		dest = parts[len(parts)-1]
	}

	ac, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	awscfg, err := awsConfigFromLocal(ctx, ac)
	if err != nil {
		log.Fatalf("AWS config: %v", err)
	}
	client := s3.NewFromConfig(awscfg)

	resp, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &ac.Bucket,
		Key:    key,
	})
	if err != nil {
		log.Fatalf("GetObject: %v", err)
	}
	defer resp.Body.Close()

	f, err := os.Create(dest)
	if err != nil {
		log.Fatalf("create %s: %v", dest, err)
	}
	defer f.Close()

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		log.Fatalf("write: %v", err)
	}
	fmt.Printf("downloaded %d bytes → %s\n", n, dest)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println(`usage:
  storacha-rclone aws-login                 # save keys/region/bucket
  storacha-rclone s3-ls [-prefix p/]        # list objects
  storacha-rclone s3-get -key k [-out f]    # download object`)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "aws-login":
		cmdAWSLogin()
	case "s3-ls":
		cmdS3List(os.Args[2:])
	case "s3-get":
		cmdS3Get(os.Args[2:])
	default:
		fmt.Println("unknown command:", os.Args[1])
		os.Exit(2)
	}
}

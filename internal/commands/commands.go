package commands

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"syscall"

	"github.com/gulshanpr/rclone/internal/aws"
	"github.com/gulshanpr/rclone/internal/config"
	"github.com/gulshanpr/rclone/internal/storacha"
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
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

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
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

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

func S3Delete(args []string) {
	fs := flag.NewFlagSet("s3-rm", flag.ExitOnError)
	key := fs.String("key", "", "object key to delete")
	prefix := fs.String("prefix", "", "prefix to delete (folder)")
	recursive := fs.Bool("recursive", false, "delete recursively (required for prefix)")
	force := fs.Bool("force", false, "skip confirmation")
	
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

	if *key == "" && *prefix == "" {
		fmt.Println("Error: must specify either -key or -prefix")
		fs.Usage()
		os.Exit(2)
	}

	if *key != "" && *prefix != "" {
		fmt.Println("Error: cannot specify both -key and -prefix")
		os.Exit(2)
	}

	ac, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Delete by prefix (folder)
	if *prefix != "" {
		if !*recursive {
			fmt.Println("Error: -recursive flag required when deleting by prefix")
			os.Exit(2)
		}

		if !*force {
			fmt.Printf("This will delete ALL objects with prefix: s3://%s/%s\n", ac.Bucket, *prefix)
			fmt.Print("Are you sure? (yes/no): ")
			var confirm string
			fmt.Scanln(&confirm)
			if confirm != "yes" {
				fmt.Println("Delete cancelled.")
				return
			}
		}

		count, err := aws.DeletePrefix(ctx, ac, *prefix)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("✓ Successfully deleted %d objects\n", count)
		return
	}

	// Delete single key - but check if it might be a prefix first
	if *recursive {
		// User specified -recursive with -key, they might mean -prefix
		fmt.Printf("Warning: -recursive flag is ignored with -key. Did you mean -prefix?\n")
		fmt.Printf("If '%s' is a folder, use: s3-rm -prefix \"%s\" -recursive\n\n", *key, *key)
	}

	if !*force {
		fmt.Printf("Delete s3://%s/%s? (yes/no): ", ac.Bucket, *key)
		var confirm string
		fmt.Scanln(&confirm)
		if confirm != "yes" {
			fmt.Println("Delete cancelled.")
			return
		}
	}

	if err := aws.DeleteObject(ctx, ac, *key); err != nil {
		log.Fatal(err)
	}
}

func StorachaLogin() {
	fmt.Println("== storacha-rclone Storacha login ==")
	fmt.Println("You need: private key (base64), proof file path, and space DID")
	fmt.Println("Generate these using: storacha key create & storacha delegation create")
	fmt.Println()

	privateKey, err := promptSecret("Private Key (base64, starts with Mg...): ")
	if err != nil {
		log.Fatal(err)
	}

	// Show the DID for this private key
	myDID, err := storacha.GetDIDFromPrivateKey(privateKey)
	if err != nil {
		log.Fatalf("invalid private key: %v", err)
	}
	fmt.Printf("\nYour DID: %s\n", myDID)
	fmt.Println("Use this DID when creating the delegation:")
	fmt.Printf("  storacha delegation create -c 'space/blob/add' -c 'space/index/add' -c 'upload/add' -c 'filecoin/offer' %s -o proof.ucan\n\n", myDID)

	proofPath, err := prompt("Proof file path (e.g., ./proof.ucan): ")
	if err != nil {
		log.Fatal(err)
	}
	spaceDID, err := prompt("Space DID (starts with did:key:...): ")
	if err != nil {
		log.Fatal(err)
	}

	cfg := config.StorachaConfig{
		PrivateKey: privateKey,
		ProofPath:  proofPath,
		SpaceDID:   spaceDID,
	}
	if err := cfg.Save(); err != nil {
		log.Fatalf("save storacha config: %v", err)
	}
	fmt.Println("Saved. (stored in ~/.storacha-rclone/storacha.json with 0600 perms)")
}

func StorachaPut(args []string) {
	fs := flag.NewFlagSet("storacha-put", flag.ExitOnError)
	filePath := fs.String("file", "", "local file path to upload (required)")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

	if *filePath == "" {
		fs.Usage()
		os.Exit(2)
	}

	cfg, err := config.LoadStoracha()
	if err != nil {
		log.Fatal(err)
	}

	client, err := storacha.NewClient(cfg)
	if err != nil {
		log.Fatalf("create storacha client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	fmt.Printf("Uploading %s to Storacha...\n", *filePath)

	cid, err := client.UploadFile(ctx, *filePath)
	if err != nil {
		log.Fatalf("upload failed: %v", err)
	}

	fmt.Printf("Upload successful!\n")
	fmt.Printf("CID: %s\n", cid)
	fmt.Printf("View at: https://w3s.link/ipfs/%s\n", cid)
}

func StorachaGet(args []string) {
	fs := flag.NewFlagSet("storacha-get", flag.ExitOnError)
	cid := fs.String("cid", "", "CID to download (required)")
	file := fs.String("file", "", "filename inside the CID directory (e.g. lol.txt)")
	outPath := fs.String("out", "", "local output path (defaults to filename)")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

	if *cid == "" {
		fmt.Println("Error: -cid is required")
		fs.Usage()
		os.Exit(2)
	}

	cfg, err := config.LoadStoracha()
	if err != nil {
		log.Fatal(err)
	}

	client, err := storacha.NewClient(cfg)
	if err != nil {
		log.Fatalf("create storacha client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	fmt.Printf("Downloading from Storacha...\n")

	if err := client.DownloadFile(ctx, *cid, *file, *outPath); err != nil {
		log.Fatalf("download failed: %v", err)
	}
}

func Copy(args []string) {
	fs := flag.NewFlagSet("cp", flag.ExitOnError)
	s3Key := fs.String("s3-key", "", "S3 object key (source for s3→storacha, dest for storacha→s3)")
	cid := fs.String("cid", "", "Storacha CID (source for storacha→s3)")
	file := fs.String("file", "", "filename inside the CID directory (for storacha→s3)")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

	// storacha → s3: needs -cid and -s3-key
	if *cid != "" {
		if *s3Key == "" {
			if *file != "" {
				*s3Key = *file
			} else {
				*s3Key = *cid
			}
		}
		copyStorachaToS3(args, *cid, *file, *s3Key)
		return
	}

	// s3 → storacha: needs -s3-key
	if *s3Key == "" {
		fmt.Println("Error: must specify either -s3-key (for s3→storacha) or -cid (for storacha→s3)")
		fs.Usage()
		os.Exit(2)
	}
	copyS3ToStoracha(*s3Key)
}

func copyS3ToStoracha(s3Key string) {
	awsCfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	storachaCfg, err := config.LoadStoracha()
	if err != nil {
		log.Fatal(err)
	}

	storachaClient, err := storacha.NewClient(storachaCfg)
	if err != nil {
		log.Fatalf("create storacha client: %v", err)
	}

	ctx := context.Background()

	fmt.Printf("Copying S3://%s/%s to Storacha...\n", awsCfg.Bucket, s3Key)

	cid, err := storachaClient.UploadFromS3(ctx, awsCfg, s3Key)
	if err != nil {
		log.Fatalf("copy failed: %v", err)
	}

	fmt.Printf("Copy successful!\n")
	fmt.Printf("CID: %s\n", cid)
	fmt.Printf("View at: https://w3s.link/ipfs/%s\n", cid)
}

func copyStorachaToS3(args []string, cid, file, s3Key string) {
	storachaCfg, err := config.LoadStoracha()
	if err != nil {
		log.Fatal(err)
	}

	awsCfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	storachaClient, err := storacha.NewClient(storachaCfg)
	if err != nil {
		log.Fatalf("create storacha client: %v", err)
	}
	defer storachaClient.Close()

	ctx := context.Background()

	fmt.Printf("Copying Storacha CID %s → s3://%s/%s\n", cid, awsCfg.Bucket, s3Key)

	reader, contentLength, err := storachaClient.DownloadToReader(ctx, cid, file)
	if err != nil {
		log.Fatalf("fetch from storacha: %v", err)
	}
	defer reader.Close()

	if contentLength > 0 {
		fmt.Printf("Streaming %d bytes to S3...\n", contentLength)
		if err := aws.UploadObject(ctx, awsCfg, s3Key, reader, contentLength); err != nil {
			log.Fatalf("upload to S3: %v", err)
		}
	} else {
		fmt.Println("Content-Length unknown, buffering before upload...")
		data, err := io.ReadAll(reader)
		if err != nil {
			log.Fatalf("read from storacha: %v", err)
		}
		fmt.Printf("Buffered %d bytes\n", len(data))
		if err := aws.UploadObject(ctx, awsCfg, s3Key, bytes.NewReader(data), int64(len(data))); err != nil {
			log.Fatalf("upload to S3: %v", err)
		}
	}

	fmt.Printf("✓ Copy complete: Storacha/%s → s3://%s/%s\n", cid, awsCfg.Bucket, s3Key)
}
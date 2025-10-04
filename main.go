package main

import (
	"fmt"
	"os"

	"github.com/gulshanpr/rclone/internal/commands"
)

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
		commands.AWSLogin()
	case "s3-ls":
		commands.S3List(os.Args[2:])
	case "s3-get":
		commands.S3Get(os.Args[2:])
	default:
		fmt.Println("unknown command:", os.Args[1])
		os.Exit(2)
	}
}
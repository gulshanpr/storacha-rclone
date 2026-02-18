package main

import (
	"fmt"
	"os"

	"github.com/gulshanpr/rclone/internal/commands"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println(`usage:
  storacha-rclone aws-login                 # save AWS keys/region/bucket
  storacha-rclone s3-ls [-prefix p/]        # list S3 objects
  storacha-rclone s3-get -key k [-out f]    # download from S3
  storacha-rclone s3-rm -key k              # delete from S3

  storacha-rclone storacha-login            # save Storacha credentials
  storacha-rclone storacha-put -file f      # upload file to Storacha
  storacha-rclone cp -s3-key k              # copy S3 object to Storacha`)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "aws-login":
		commands.AWSLogin()
	case "s3-ls":
		commands.S3List(os.Args[2:])
	case "s3-get":
		commands.S3Get(os.Args[2:])
	case "s3-rm":
		commands.S3Delete(os.Args[2:])
	case "storacha-login":
		commands.StorachaLogin()
	case "storacha-put":
		commands.StorachaPut(os.Args[2:])
	case "cp":
		commands.Copy(os.Args[2:])
	default:
		fmt.Println("unknown command:", os.Args[1])
		os.Exit(2)
	}
}

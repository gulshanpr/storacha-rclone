# Storacha rclone

## aws auth and getting oject
Build it first
> go build

Login using `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `region` and `bucket-name` (need only AmazonS3ReadOnlyAccess)
```
./bin/rclone aws-login
```

List the objects in the bucket
```
./bin/rclone s3-ls
```

Download the files with key
```
./bin/rclone s3-get
```

Delete files or folders from S3
```bash
# Delete a single file (with confirmation)
./bin/rclone s3-rm -key "path/to/file.txt"

# Delete a single file (skip confirmation)
./bin/rclone s3-rm -key "path/to/file.txt" -force

# Delete a folder and all its contents (requires -recursive)
./bin/rclone s3-rm -prefix "path/to/folder/" -recursive

# Delete folder without confirmation (use with caution!)
./bin/rclone s3-rm -prefix "path/to/folder/" -recursive -force
```

**Note**: 
- Use `-key` for single files
- Use `-prefix` with `-recursive` for folders
- The `-force` flag skips confirmation prompts
- Prefix deletion uses batch operations (1000 objects per API call)

### Download from Storacha
```bash
# Download a file from a directory CID (most common — storacha wraps uploads in a dir)
./bin/rclone storacha-get -cid  -file  -out 

# Example
./bin/rclone storacha-get \
  -cid bafybeihjnbauyqkqq4xibszzm3jpjf4vhlgb2yddabqodnrmcbvdnihyau \
  -file lol2.txt \
  -out downloaded.txt
```

**Notes:**
- `-cid` is the CID printed by `storacha-put` (required)
- `-file` is the original filename inside the uploaded directory (required for directory CIDs)
- `-out` is the local path to save to (defaults to the value of `-file`)
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

## Copy between S3 and Storacha

The `cp` command supports copying files in both directions between S3 and Storacha.

### S3 → Storacha

Copies an object from your configured S3 bucket directly to Storacha (streamed, no temp file).
```bash
./bin/rclone cp -s3-key "path/to/file.txt"
```

### Storacha → S3

Copies a file from Storacha (via IPFS gateway) into your configured S3 bucket.
```bash
# Copy a file from a directory CID (most common — storacha wraps uploads in a dir)
./bin/rclone cp \
  -cid bafybeihjnbauyqkqq4xibszzm3jpjf4vhlgb2yddabqodnrmcbvdnihyau \
  -file dog.txt \
  -s3-key backups/dog.txt

# Copy a raw file CID (no -file needed — e.g. when uploaded via stdin/s3-key)
./bin/rclone cp \
  -cid bafkreiaipjxl4fc54n3cgm63xl4ka3b4fkz2a26kehydnq3ljabtk6jbne \
  -s3-key yo.webp

# -s3-key is optional — defaults to the value of -file
./bin/rclone cp \
  -cid bafybeihjnbauyqkqq4xibszzm3jpjf4vhlgb2yddabqodnrmcbvdnihyau \
  -file dog.txt
```

**Notes:**
- `-cid` is the CID printed by `storacha-put` or `cp` (required)
- `-file` is the original filename inside the uploaded directory (only needed for directory CIDs)
- `-s3-key` is the destination key in S3 (defaults to `-file` value if omitted)
- Raw file CIDs (e.g. `bafkrei...`) from stdin/S3 uploads don't need `-file`
- Content is buffered in memory before upload to satisfy S3's `Content-Length` requirement
- Falls back to `storacha.link` gateway if `w3s.link` is unavailable

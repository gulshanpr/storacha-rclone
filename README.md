# Storacha rclone

## aws auth and getting oject
Build it first
> go build

Login using `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `region` and `bucket-name` (need only AmazonS3ReadOnlyAccess)
```
../bin/rclone aws-login
```

List the objects in the bucket
```
./bin/rclone s3-ls
```

Download the files with key
```
./bin/rclone s3-get
```
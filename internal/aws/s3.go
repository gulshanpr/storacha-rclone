package aws

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	appconfig "github.com/gulshanpr/rclone/internal/config"
)

func ConfigFromLocal(ctx context.Context, ac appconfig.AppConfig) (aws.Config, error) {
	creds := credentials.NewStaticCredentialsProvider(ac.AccessKeyID, ac.SecretAccessKey, "")
	return config.LoadDefaultConfig(ctx,
		config.WithRegion(ac.Region),
		config.WithCredentialsProvider(creds),
	)
}

func ListObjects(ctx context.Context, ac appconfig.AppConfig, prefix *string) error {
	awscfg, err := ConfigFromLocal(ctx, ac)
	if err != nil {
		return fmt.Errorf("AWS config: %w", err)
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
			return fmt.Errorf("ListObjectsV2: %w", err)
		}
		for _, obj := range out.Contents {
			fmt.Printf("%12d  %s\n", obj.Size, aws.ToString(obj.Key))
		}
		if out.IsTruncated == nil || !*out.IsTruncated {
			break
		}
		token = out.NextContinuationToken
	}
	return nil
}

func DownloadObject(ctx context.Context, ac appconfig.AppConfig, key, dest string) (err error) {
	if dest == "" {
		if i := strings.LastIndex(key, "/"); i >= 0 {
			dest = key[i+1:]
		} else {
			dest = key
		}
	}

	awscfg, err := ConfigFromLocal(ctx, ac)
	if err != nil {
		return fmt.Errorf("AWS config: %w", err)
	}
	client := s3.NewFromConfig(awscfg)

	resp, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &ac.Bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("GetObject: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); err == nil {
			err = closeErr
		}
	}()

	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}
	fmt.Printf("downloaded %d bytes → %s\n", n, dest)
	return nil
}

func UploadObject(ctx context.Context, ac appconfig.AppConfig, key string, body io.Reader, contentLength int64) error {
	awscfg, err := ConfigFromLocal(ctx, ac)
	if err != nil {
		return fmt.Errorf("AWS config: %w", err)
	}
	client := s3.NewFromConfig(awscfg)

	input := &s3.PutObjectInput{
		Bucket: &ac.Bucket,
		Key:    &key,
		Body:   body,
	}
	if contentLength > 0 {
		input.ContentLength = &contentLength
	}

	if _, err = client.PutObject(ctx, input); err != nil {
		return fmt.Errorf("PutObject: %w", err)
	}

	fmt.Printf("✓ Uploaded to s3://%s/%s\n", ac.Bucket, key)
	return nil
}

func DeleteObject(ctx context.Context, ac appconfig.AppConfig, key string) error {
	awscfg, err := ConfigFromLocal(ctx, ac)
	if err != nil {
		return fmt.Errorf("AWS config: %w", err)
	}
	client := s3.NewFromConfig(awscfg)

	if _, err = client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: &ac.Bucket,
		Key:    &key,
	}); err != nil {
		return fmt.Errorf("object not found: s3://%s/%s: %w", ac.Bucket, key, err)
	}

	if _, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &ac.Bucket,
		Key:    &key,
	}); err != nil {
		return fmt.Errorf("DeleteObject: %w", err)
	}

	fmt.Printf("✓ Deleted: s3://%s/%s\n", ac.Bucket, key)
	return nil
}

func DeletePrefix(ctx context.Context, ac appconfig.AppConfig, prefix string) (int, error) {
	awscfg, err := ConfigFromLocal(ctx, ac)
	if err != nil {
		return 0, fmt.Errorf("AWS config: %w", err)
	}
	client := s3.NewFromConfig(awscfg)

	const batchSize = 1000
	deleted := 0
	var token *string

	for {
		out, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            &ac.Bucket,
			Prefix:            &prefix,
			ContinuationToken: token,
			MaxKeys:           aws.Int32(batchSize),
		})
		if err != nil {
			return deleted, fmt.Errorf("ListObjectsV2: %w", err)
		}

		if len(out.Contents) == 0 {
			break
		}

		ids := make([]types.ObjectIdentifier, len(out.Contents))
		for i, obj := range out.Contents {
			ids[i] = types.ObjectIdentifier{Key: obj.Key}
		}

		if _, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: &ac.Bucket,
			Delete: &types.Delete{
				Objects: ids,
				Quiet:   aws.Bool(true),
			},
		}); err != nil {
			return deleted, fmt.Errorf("DeleteObjects batch: %w", err)
		}

		deleted += len(ids)
		fmt.Printf("Deleted %d objects...\n", deleted)

		if out.IsTruncated == nil || !*out.IsTruncated {
			break
		}
		token = out.NextContinuationToken
	}

	if deleted == 0 {
		return 0, fmt.Errorf("no objects found with prefix: %s", prefix)
	}

	return deleted, nil
}

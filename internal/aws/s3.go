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
		return fmt.Errorf("AWS config: %v", err)
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
			return fmt.Errorf("ListObjectsV2: %v", err)
		}
		for _, obj := range out.Contents {
			size := obj.Size
			key := *obj.Key
			fmt.Printf("%12d  %s\n", size, key)
		}
		if *out.IsTruncated {
			token = out.NextContinuationToken
			continue
		}
		break
	}
	return nil
}

func DownloadObject(ctx context.Context, ac appconfig.AppConfig, key, dest string) (err error) {
	if dest == "" {
		parts := strings.Split(key, "/")
		dest = parts[len(parts)-1]
	}

	awscfg, err := ConfigFromLocal(ctx, ac)
	if err != nil {
		return fmt.Errorf("AWS config: %v", err)
	}
	client := s3.NewFromConfig(awscfg)

	resp, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &ac.Bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("GetObject: %v", err)
	}
	defer func() {
		closeErr := resp.Body.Close()
		if err == nil {
			err = closeErr
		}
	}()

	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create %s: %v", dest, err)
	}
	defer func() {
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
	}()

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return fmt.Errorf("write: %v", err)
	}
	fmt.Printf("downloaded %d bytes → %s\n", n, dest)
	return nil
}

// internal/aws/s3.go

// DeleteObject deletes a single object
func DeleteObject(ctx context.Context, ac appconfig.AppConfig, key string) error {
	awscfg, err := ConfigFromLocal(ctx, ac)
	if err != nil {
		return fmt.Errorf("AWS config: %v", err)
	}
	client := s3.NewFromConfig(awscfg)

	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &ac.Bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("DeleteObject: %v", err)
	}

	fmt.Printf("✓ Deleted: s3://%s/%s\n", ac.Bucket, key)
	return nil
}

// DeletePrefix deletes all objects with a given prefix (folder)
func DeletePrefix(ctx context.Context, ac appconfig.AppConfig, prefix string) (int, error) {
	awscfg, err := ConfigFromLocal(ctx, ac)
	if err != nil {
		return 0, fmt.Errorf("AWS config: %v", err)
	}
	client := s3.NewFromConfig(awscfg)

	// List all objects with prefix
	var objectsToDelete []string
	var token *string
	
	for {
		out, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            &ac.Bucket,
			Prefix:            &prefix,
			ContinuationToken: token,
		})
		if err != nil {
			return 0, fmt.Errorf("ListObjectsV2: %v", err)
		}
		
		for _, obj := range out.Contents {
			objectsToDelete = append(objectsToDelete, *obj.Key)
		}
		
		if !*out.IsTruncated {
			break
		}
		token = out.NextContinuationToken
	}

	if len(objectsToDelete) == 0 {
		return 0, fmt.Errorf("no objects found with prefix: %s", prefix)
	}

	// Batch delete (max 1000 per request)
	deleted := 0
	for i := 0; i < len(objectsToDelete); i += 1000 {
		end := i + 1000
		if end > len(objectsToDelete) {
			end = len(objectsToDelete)
		}
		
		batch := objectsToDelete[i:end]
		var objectIdentifiers []types.ObjectIdentifier
		for _, key := range batch {
			k := key
			objectIdentifiers = append(objectIdentifiers, types.ObjectIdentifier{
				Key: &k,
			})
		}
		
		_, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: &ac.Bucket,
			Delete: &types.Delete{
				Objects: objectIdentifiers,
				Quiet:   aws.Bool(false),
			},
		})
		if err != nil {
			return deleted, fmt.Errorf("DeleteObjects batch: %v", err)
		}
		
		deleted += len(batch)
		fmt.Printf("Deleted %d objects...\n", deleted)
	}

	return deleted, nil
}


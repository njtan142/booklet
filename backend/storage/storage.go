package storage

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"strconv"
	"time"

	"booklet/logger"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var MinioClient *minio.Client
var BucketName string

func InitStorage() error {
	endpoint := os.Getenv("MINIO_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:9000"
	}

	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	if accessKey == "" {
		accessKey = "minioadmin"
	}

	secretKey := os.Getenv("MINIO_SECRET_KEY")
	if secretKey == "" {
		secretKey = "minioadmin"
	}

	useSSLStr := os.Getenv("MINIO_USE_SSL")
	useSSL := false
	if useSSLStr != "" {
		var err error
		useSSL, err = strconv.ParseBool(useSSLStr)
		if err != nil {
			log.Printf("Warning: failed to parse MINIO_USE_SSL: %v. Defaulting to false.", err)
		}
	}

	BucketName = os.Getenv("MINIO_BUCKET")
	if BucketName == "" {
		BucketName = "booklet"
	}

	var client *minio.Client
	var err error

	// Retry connection on startup (important for docker-compose startup timing)
	for i := 1; i <= 10; i++ {
		log.Printf("Connecting to MinIO (attempt %d/10)...", i)
		client, err = minio.New(endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
			Secure: useSSL,
		})
		if err == nil {
			// Test connection by listing buckets
			_, err = client.ListBuckets(context.Background())
		}
		if err == nil {
			break
		}
		log.Printf("MinIO is not ready yet: %v. Retrying in 3 seconds...", err)
		time.Sleep(3 * time.Second)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to MinIO: %w", err)
	}

	MinioClient = client
	log.Println("MinIO connection established.")

	// Create bucket if it doesn't exist
	ctx := context.Background()
	exists, err := MinioClient.BucketExists(ctx, BucketName)
	if err != nil {
		return fmt.Errorf("failed to check if bucket exists: %w", err)
	}

	if !exists {
		log.Printf("Creating bucket '%s'...", BucketName)
		err = MinioClient.MakeBucket(ctx, BucketName, minio.MakeBucketOptions{})
		if err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
		log.Printf("Bucket '%s' created successfully.", BucketName)
	} else {
		log.Printf("Bucket '%s' already exists.", BucketName)
	}

	return nil
}

// UploadFile uploads a file from local disk to MinIO
func UploadFile(ctx context.Context, objectName string, filePath string, contentType string) error {
	_, err := MinioClient.FPutObject(ctx, BucketName, objectName, filePath, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("failed to upload file %s: %w", filePath, err)
	}
	logger.Logf(ctx, "Uploaded %s to MinIO as %s", filePath, objectName)
	return nil
}

// UploadStream uploads a stream to MinIO
func UploadStream(ctx context.Context, objectName string, reader io.Reader, size int64, contentType string) error {
	_, err := MinioClient.PutObject(ctx, BucketName, objectName, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("failed to upload stream to %s: %w", objectName, err)
	}
	logger.Logf(ctx, "Uploaded stream to MinIO as %s (size: %d bytes)", objectName, size)
	return nil
}

// DownloadFile downloads a file from MinIO to local disk
func DownloadFile(ctx context.Context, objectName string, destPath string) error {
	err := MinioClient.FGetObject(ctx, BucketName, objectName, destPath, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to download %s to %s: %w", objectName, destPath, err)
	}
	logger.Logf(ctx, "Downloaded %s from MinIO to %s", objectName, destPath)
	return nil
}

// GetDownloadURL generates a signed URL for direct client-side downloads (valid for 1 hour)
func GetDownloadURL(ctx context.Context, objectName string) (string, error) {
	reqParams := make(url.Values)
	// Optionally set content disposition
	// reqParams.Set("response-content-disposition", "attachment; filename=\"booklet.pdf\"")
	
	presignedURL, err := MinioClient.PresignedGetObject(ctx, BucketName, objectName, time.Hour, reqParams)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL for %s: %w", objectName, err)
	}
	return presignedURL.String(), nil
}

// DeleteFile deletes an object from MinIO
func DeleteFile(ctx context.Context, objectName string) error {
	err := MinioClient.RemoveObject(ctx, BucketName, objectName, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete %s from MinIO: %w", objectName, err)
	}
	logger.Logf(ctx, "Deleted %s from MinIO", objectName)
	return nil
}

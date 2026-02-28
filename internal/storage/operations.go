package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// HeadResult contains metadata from a HEAD request.
type HeadResult struct {
	ContentLength int64
	LastModified  time.Time
	VersionID     string
	Metadata      map[string]string
}

// VersionInfo describes a single object version.
type VersionInfo struct {
	VersionID    string
	LastModified time.Time
	Size         int64
	IsLatest     bool
}

// Upload uploads a file to S3 with metadata, returning the version ID if available.
func (c *Client) Upload(ctx context.Context, key string, spoolPath string, metadata map[string]string) (string, error) {
	f, err := os.Open(spoolPath)
	if err != nil {
		return "", fmt.Errorf("opening spool file: %w", err)
	}
	defer f.Close()

	uploader := manager.NewUploader(c.S3)
	result, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:   aws.String(c.Bucket),
		Key:      aws.String(key),
		Body:     f,
		Metadata: metadata,
	})
	if err != nil {
		return "", fmt.Errorf("uploading to %s/%s: %w", c.Bucket, key, err)
	}

	versionID := ""
	if result.VersionID != nil {
		versionID = *result.VersionID
	}
	return versionID, nil
}

// Download downloads an object from S3 to a writer. If versionID is empty, downloads latest.
func (c *Client) Download(ctx context.Context, key, versionID string, dst io.Writer) error {
	input := &s3.GetObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(key),
	}
	if versionID != "" {
		input.VersionId = aws.String(versionID)
	}

	result, err := c.S3.GetObject(ctx, input)
	if err != nil {
		return fmt.Errorf("downloading %s/%s: %w", c.Bucket, key, err)
	}
	defer result.Body.Close()

	if _, err := io.Copy(dst, result.Body); err != nil {
		return fmt.Errorf("reading %s/%s: %w", c.Bucket, key, err)
	}
	return nil
}

// Head returns metadata for an object.
func (c *Client) Head(ctx context.Context, key, versionID string) (*HeadResult, error) {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(key),
	}
	if versionID != "" {
		input.VersionId = aws.String(versionID)
	}

	result, err := c.S3.HeadObject(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("head %s/%s: %w", c.Bucket, key, err)
	}

	hr := &HeadResult{
		Metadata: result.Metadata,
	}
	if result.ContentLength != nil {
		hr.ContentLength = *result.ContentLength
	}
	if result.LastModified != nil {
		hr.LastModified = *result.LastModified
	}
	if result.VersionId != nil {
		hr.VersionID = *result.VersionId
	}
	return hr, nil
}

// HeadBucket checks if the bucket exists and is accessible.
func (c *Client) HeadBucket(ctx context.Context) error {
	_, err := c.S3.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(c.Bucket),
	})
	if err != nil {
		return fmt.Errorf("head bucket %s: %w", c.Bucket, err)
	}
	return nil
}

// ListVersions returns all versions for the given exact key, newest first.
func (c *Client) ListVersions(ctx context.Context, key string) ([]VersionInfo, error) {
	input := &s3.ListObjectVersionsInput{
		Bucket: aws.String(c.Bucket),
		Prefix: aws.String(key),
	}

	var versions []VersionInfo
	paginator := s3.NewListObjectVersionsPaginator(c.S3, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing versions for %s/%s: %w", c.Bucket, key, err)
		}
		for _, v := range page.Versions {
			if aws.ToString(v.Key) != key {
				continue
			}
			vi := VersionInfo{
				VersionID: aws.ToString(v.VersionId),
				IsLatest:  aws.ToBool(v.IsLatest),
			}
			if v.LastModified != nil {
				vi.LastModified = *v.LastModified
			}
			if v.Size != nil {
				vi.Size = *v.Size
			}
			versions = append(versions, vi)
		}
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].LastModified.After(versions[j].LastModified)
	})
	return versions, nil
}

// UploadBytes uploads a small byte slice to S3.
func (c *Client) UploadBytes(ctx context.Context, key string, data io.Reader, metadata map[string]string) (string, error) {
	result, err := c.S3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:   aws.String(c.Bucket),
		Key:      aws.String(key),
		Body:     data,
		Metadata: metadata,
	})
	if err != nil {
		return "", fmt.Errorf("uploading to %s/%s: %w", c.Bucket, key, err)
	}

	versionID := ""
	if result.VersionId != nil {
		versionID = *result.VersionId
	}
	return versionID, nil
}

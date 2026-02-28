package storage

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/kolontsov/rbackup/internal/config"
)

type ClientOptions struct {
	AttemptTimeout time.Duration
	MaxAttempts    int
}

// Client wraps an S3 client for a specific remote.
type Client struct {
	S3     *s3.Client
	Bucket string
	Name   string
}

// NewClient creates an S3 client configured for the given remote.
func NewClient(ctx context.Context, remote config.Remote) (*Client, error) {
	return NewClientWithOptions(ctx, remote, ClientOptions{})
}

// NewClientWithOptions creates an S3 client configured for the given remote and request behavior.
func NewClientWithOptions(ctx context.Context, remote config.Remote, options ClientOptions) (*Client, error) {
	var opts []func(*awsconfig.LoadOptions) error

	if remote.Region != "" {
		opts = append(opts, awsconfig.WithRegion(remote.Region))
	}

	if remote.AWSProfile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(remote.AWSProfile))
	}

	if remote.AccessKeyID != "" && remote.SecretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(remote.AccessKeyID, remote.SecretAccessKey, ""),
		))
	}
	if options.MaxAttempts > 0 {
		opts = append(opts, awsconfig.WithRetryMaxAttempts(options.MaxAttempts))
	}
	if options.AttemptTimeout > 0 {
		opts = append(opts, awsconfig.WithHTTPClient(&http.Client{
			Timeout: options.AttemptTimeout,
		}))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config for remote %q: %w", remote.Name, err)
	}

	var s3Opts []func(*s3.Options)

	if remote.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(remote.Endpoint)
			o.UsePathStyle = true // force path-style for custom endpoints (B2, MinIO)
		})
	}

	client := s3.NewFromConfig(cfg, s3Opts...)

	return &Client{
		S3:     client,
		Bucket: remote.Bucket,
		Name:   remote.Name,
	}, nil
}

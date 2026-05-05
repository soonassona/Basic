// Package storage adapts the AWS S3 v2 SDK to the application.ObjectStore
// port. Cloudflare R2 is S3-compatible; locally we point at MinIO with
// path-style URLs.
package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/domain"
)

type R2 struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
}

type Options struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	ForcePathStyle  bool
}

// New configures an S3 v2 client against the given endpoint. R2 honours the
// AWS S3 API and requires `region=auto`; MinIO accepts any region.
func New(ctx context.Context, opts Options) (*R2, error) {
	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(opts.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			opts.AccessKeyID, opts.SecretAccessKey, "",
		)),
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}

	s3Opts := []func(*s3.Options){}
	if opts.Endpoint != "" {
		ep := opts.Endpoint
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(ep)
		})
	}
	if opts.ForcePathStyle {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(cfg, s3Opts...)
	return &R2{
		client:    client,
		presigner: s3.NewPresignClient(client),
		bucket:    opts.Bucket,
	}, nil
}

// PresignPut returns a time-limited URL the client uses to upload directly
// to R2/MinIO. The Content-Type and Content-Length conditions are encoded
// into the signed URL — clients must send the same values.
func (r *R2) PresignPut(ctx context.Context, key, contentType string, byteSize int64, ttl time.Duration) (application.PresignedURL, error) {
	req, err := r.presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(r.bucket),
		Key:           aws.String(key),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(byteSize),
	}, func(po *s3.PresignOptions) {
		po.Expires = ttl
	})
	if err != nil {
		return application.PresignedURL{}, fmt.Errorf("presign put: %w", err)
	}
	return application.PresignedURL{
		URL:    req.URL,
		Method: req.Method,
		Headers: map[string]string{
			"Content-Type":   contentType,
			"Content-Length": fmt.Sprintf("%d", byteSize),
		},
		Expires: time.Now().Add(ttl),
	}, nil
}

// HeadObject probes the uploaded object so FinalizeUpload can verify the
// declared byte size matches what actually landed in storage.
func (r *R2) HeadObject(ctx context.Context, key string) (application.ObjectInfo, error) {
	out, err := r.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		// S3 v2 surfaces 404s as smithy errors; treat any HEAD failure
		// against an uploading row as "not found yet" for the caller.
		if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "StatusCode: 404") {
			return application.ObjectInfo{}, domain.ErrNotFound
		}
		return application.ObjectInfo{}, fmt.Errorf("head object: %w", err)
	}
	info := application.ObjectInfo{}
	if out.ETag != nil {
		info.ETag = strings.Trim(*out.ETag, `"`)
	}
	if out.ContentType != nil {
		info.ContentType = *out.ContentType
	}
	if out.ContentLength != nil {
		info.ByteSize = *out.ContentLength
	}
	return info, nil
}

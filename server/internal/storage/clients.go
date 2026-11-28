package storage

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type DualStorage struct {
	b2Client       *s3.Client
	supabaseClient *s3.Client
	b2             *s3.PresignClient
	supabase       *s3.PresignClient
	b2Bucket       string
	supabaseBucket string
	publicBaseURL  string
}

func NewDualStorage(ctx context.Context) (*DualStorage, error) {
	b2Client, b2, err := newS3Client(ctx, "B2_REGION", "B2_ENDPOINT", "B2_KEY_ID", "B2_APP_KEY", false)
	if err != nil {
		return nil, err
	}
	supabaseClient, supabase, err := newS3Client(ctx, "SUPABASE_S3_REGION", "SUPABASE_S3_ENDPOINT", "SUPABASE_S3_KEY_ID", "SUPABASE_S3_SECRET", true)
	if err != nil {
		return nil, err
	}
	b2Bucket, supabaseBucket := env("B2_BUCKET"), env("SUPABASE_BUCKET")
	publicBaseURL := strings.TrimRight(env("SUPABASE_URL"), "/")
	if b2Bucket == "" || supabaseBucket == "" || publicBaseURL == "" {
		return nil, fmt.Errorf("B2_BUCKET, SUPABASE_BUCKET, and SUPABASE_URL are required")
	}
	return &DualStorage{b2Client: b2Client, supabaseClient: supabaseClient, b2: b2, supabase: supabase, b2Bucket: b2Bucket, supabaseBucket: supabaseBucket, publicBaseURL: publicBaseURL}, nil
}

func newS3Client(ctx context.Context, regionEnv, endpointEnv, keyIDEnv, secretEnv string, pathStyle bool) (*s3.Client, *s3.PresignClient, error) {
	region, endpoint, keyID, secret := env(regionEnv), env(endpointEnv), env(keyIDEnv), env(secretEnv)
	if region == "" || endpoint == "" || keyID == "" || secret == "" {
		return nil, nil, fmt.Errorf("%s, %s, %s, and %s are required", regionEnv, endpointEnv, keyIDEnv, secretEnv)
	}
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(keyID, secret, "")),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("load %s configuration: %w", regionEnv, err)
	}
	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = pathStyle
	})
	return client, s3.NewPresignClient(client), nil
}

func (s *DualStorage) DeletePhotoObjects(ctx context.Context, groupID, photoID, originalKey string) error {
	keys := []struct {
		client *s3.Client
		bucket string
		key    string
	}{
		{s.supabaseClient, s.supabaseBucket, fmt.Sprintf("thumbs/%s/%s.webp", groupID, photoID)},
		{s.supabaseClient, s.supabaseBucket, fmt.Sprintf("previews/%s/%s.webp", groupID, photoID)},
		{s.b2Client, s.b2Bucket, originalKey},
	}
	for _, object := range keys {
		_, err := object.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(object.bucket),
			Key:    aws.String(object.key),
		})
		if err != nil {
			return fmt.Errorf("delete storage object %q: %w", object.key, err)
		}
	}
	return nil
}

func (s *DualStorage) PresignImageUploads(ctx context.Context, groupID, photoID, ext, originalContentType string, originalSize int64) (thumbUploadURL, previewUploadURL, originalUploadURL, thumbURL, previewURL, originalKey string, err error) {
	thumbKey := fmt.Sprintf("thumbs/%s/%s.webp", groupID, photoID)
	previewKey := fmt.Sprintf("previews/%s/%s.webp", groupID, photoID)
	originalKey = fmt.Sprintf("originals/%s/%s.%s", groupID, photoID, ext)
	thumbPresign, err := s.supabase.PresignPutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.supabaseBucket), Key: aws.String(thumbKey), ContentType: aws.String("image/webp")}, func(options *s3.PresignOptions) { options.Expires = 5 * time.Minute })
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("presign thumbnail upload: %w", err)
	}
	previewPresign, err := s.supabase.PresignPutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.supabaseBucket), Key: aws.String(previewKey), ContentType: aws.String("image/webp")}, func(options *s3.PresignOptions) { options.Expires = 5 * time.Minute })
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("presign preview upload: %w", err)
	}
	originalPresign, err := s.b2.PresignPutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.b2Bucket), Key: aws.String(originalKey), ContentType: aws.String(originalContentType), ContentLength: aws.Int64(originalSize)}, func(options *s3.PresignOptions) { options.Expires = 5 * time.Minute })
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("presign original upload: %w", err)
	}
	return thumbPresign.URL, previewPresign.URL, originalPresign.URL, s.publicURL(thumbKey), s.publicURL(previewKey), originalKey, nil
}

func (s *DualStorage) PresignOriginalDownload(ctx context.Context, originalKey, photoID string) (string, error) {
	ext := path.Ext(originalKey)
	if ext != ".jpg" && ext != ".png" && ext != ".webp" {
		ext = ".jpg"
	}
	response := fmt.Sprintf(`attachment; filename="pickme-%s%s"`, photoID, ext)
	presigned, err := s.b2.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.b2Bucket), Key: aws.String(originalKey), ResponseContentDisposition: aws.String(response)}, func(options *s3.PresignOptions) { options.Expires = 2 * time.Minute })
	if err != nil {
		return "", fmt.Errorf("presign original download: %w", err)
	}
	return presigned.URL, nil
}

func (s *DualStorage) PublicImageURLs(groupID, photoID string) (thumbURL, previewURL string) {
	return s.publicURL(fmt.Sprintf("thumbs/%s/%s.webp", groupID, photoID)), s.publicURL(fmt.Sprintf("previews/%s/%s.webp", groupID, photoID))
}

func (s *DualStorage) ValidOriginalKey(groupID, photoID, key string) bool {
	for _, ext := range []string{"jpg", "png", "webp"} {
		if key == fmt.Sprintf("originals/%s/%s.%s", groupID, photoID, ext) {
			return true
		}
	}
	return false
}

func (s *DualStorage) publicURL(key string) string {
	segments := strings.Split(key, "/")
	for i := range segments {
		segments[i] = url.PathEscape(segments[i])
	}
	return s.publicBaseURL + "/storage/v1/object/public/" + url.PathEscape(s.supabaseBucket) + "/" + strings.Join(segments, "/")
}

func env(key string) string { return strings.TrimSpace(os.Getenv(key)) }

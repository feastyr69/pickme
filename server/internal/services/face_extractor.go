package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxExtractorResponseSize = 8 << 20

type DetectedFace struct {
	BBox      []int     `json:"bbox"`
	DetScore  float64   `json:"det_score"`
	Embedding []float32 `json:"embedding"`
}

type FaceExtractor interface {
	Extract(context.Context, string) ([]DetectedFace, error)
}

type FaceExtractorClient struct {
	endpoint string
	client   *http.Client
}

func NewFaceExtractorClient(baseURL string) (*FaceExtractorClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:8000"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid face extractor URL %q", baseURL)
	}
	return &FaceExtractorClient{
		endpoint: baseURL + "/extract",
		client:   &http.Client{Timeout: 90 * time.Second},
	}, nil
}

func (c *FaceExtractorClient) Extract(ctx context.Context, imageURL string) ([]DetectedFace, error) {
	body, err := json.Marshal(struct {
		ImageURL string `json:"image_url"`
	}{ImageURL: imageURL})
	if err != nil {
		return nil, fmt.Errorf("encode face extraction request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create face extraction request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call face extractor: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("face extractor returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}

	var result struct {
		Faces *[]DetectedFace `json:"faces"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxExtractorResponseSize))
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode face extractor response: %w", err)
	}
	if result.Faces == nil {
		return nil, fmt.Errorf("face extractor response is missing faces")
	}
	return *result.Faces, nil
}

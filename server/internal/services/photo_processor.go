package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/feastyr69/pickme/server/internal/store"
)

type FaceEmbeddingWriter interface {
	SetPhotoStatus(context.Context, string, string) error
	ReplaceFaceEmbeddings(context.Context, string, string, []store.FaceEmbedding, float64) error
}

type PhotoProcessor struct {
	Extractor FaceExtractor
	Photos    FaceEmbeddingWriter
	Threshold float64
}

func (p *PhotoProcessor) Process(ctx context.Context, job ImageJob) error {
	if err := p.Photos.SetPhotoStatus(ctx, job.PhotoID, store.PhotoStatusProcessing); err != nil {
		if errors.Is(err, store.ErrPhotoNotFound) {
			return nil
		}
		return fmt.Errorf("mark photo %s processing: %w", job.PhotoID, err)
	}
	if err := p.process(ctx, job); err != nil {
		statusCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if statusErr := p.Photos.SetPhotoStatus(statusCtx, job.PhotoID, store.PhotoStatusFailed); statusErr != nil {
			if errors.Is(statusErr, store.ErrPhotoNotFound) {
				return nil
			}
			return fmt.Errorf("%w (also failed to mark photo failed: %v)", err, statusErr)
		}
		return err
	}
	return nil
}

func (p *PhotoProcessor) process(ctx context.Context, job ImageJob) error {
	faces, err := p.Extractor.Extract(ctx, job.PreviewURL)
	if err != nil {
		return fmt.Errorf("extract faces for photo %s: %w", job.PhotoID, err)
	}

	embeddings := make([]store.FaceEmbedding, 0, len(faces))
	for index, face := range faces {
		if len(face.BBox) != 4 {
			return fmt.Errorf("face %d for photo %s has %d bounding-box values; expected 4", index, job.PhotoID, len(face.BBox))
		}
		if len(face.Embedding) != 512 {
			return fmt.Errorf("face %d for photo %s has %d embedding values; expected 512", index, job.PhotoID, len(face.Embedding))
		}
		bbox := make([]int32, len(face.BBox))
		for i, coordinate := range face.BBox {
			bbox[i] = int32(coordinate)
		}
		for _, value := range face.Embedding {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return fmt.Errorf("face %d for photo %s has a non-finite embedding value", index, job.PhotoID)
			}
		}
		embeddings = append(embeddings, store.FaceEmbedding{BBox: bbox, Embedding: face.Embedding})
	}

	if math.IsNaN(p.Threshold) || math.IsInf(p.Threshold, 0) || p.Threshold <= 0 || p.Threshold > 1 {
		return fmt.Errorf("face match threshold must be greater than 0 and at most 1")
	}
	if err := p.Photos.ReplaceFaceEmbeddings(ctx, job.PhotoID, job.GroupID, embeddings, p.Threshold); err != nil {
		return fmt.Errorf("save faces for photo %s: %w", job.PhotoID, err)
	}
	return nil
}

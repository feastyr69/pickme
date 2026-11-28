package services

import (
	"context"
	"fmt"
	"math"

	"github.com/feastyr69/pickme/server/internal/store"
)

type FaceEmbeddingWriter interface {
	ReplaceFaceEmbeddings(context.Context, string, string, []store.FaceEmbedding) error
}

type PhotoProcessor struct {
	Extractor FaceExtractor
	Photos    FaceEmbeddingWriter
}

func (p *PhotoProcessor) Process(ctx context.Context, job ImageJob) error {
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

	if err := p.Photos.ReplaceFaceEmbeddings(ctx, job.PhotoID, job.GroupID, embeddings); err != nil {
		return fmt.Errorf("save faces for photo %s: %w", job.PhotoID, err)
	}
	return nil
}

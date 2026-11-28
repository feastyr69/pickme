package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
)

type Photo struct {
	ID          string    `json:"id"`
	GroupID     string    `json:"group_id"`
	ThumbURL    string    `json:"thumb_url"`
	PreviewURL  string    `json:"preview_url"`
	OriginalKey string    `json:"original_key,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type PhotoStore struct{ Pool *pgxpool.Pool }

type FaceEmbedding struct {
	BBox      []int32
	Embedding []float32
}

func (s *PhotoStore) CreatePhoto(ctx context.Context, photo Photo) (*Photo, error) {
	query := `
	INSERT INTO photos (id, group_id, thumb_url, preview_url, original_key, status)
	VALUES ($1, $2, $3, $4, $5, 'pending')
	ON CONFLICT (id) DO UPDATE SET
		thumb_url = EXCLUDED.thumb_url,
		preview_url = EXCLUDED.preview_url,
		original_key = EXCLUDED.original_key
	WHERE photos.group_id = EXCLUDED.group_id
	RETURNING id, group_id, thumb_url, preview_url, original_key, status, created_at
	`
	var created Photo
	err := s.Pool.QueryRow(ctx, query, photo.ID, photo.GroupID, photo.ThumbURL, photo.PreviewURL, photo.OriginalKey).Scan(
		&created.ID, &created.GroupID, &created.ThumbURL, &created.PreviewURL, &created.OriginalKey, &created.Status, &created.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create photo: %w", err)
	}
	return &created, nil
}

func (s *PhotoStore) GetPhotoByID(ctx context.Context, id string) (*Photo, error) {
	return scanPhoto(s.Pool.QueryRow(ctx, `SELECT id, group_id, thumb_url, preview_url, original_key, status, created_at FROM photos WHERE id = $1`, id))
}

func (s *PhotoStore) GetPhotoByIDForUser(ctx context.Context, id, userID string) (*Photo, error) {
	return scanPhoto(s.Pool.QueryRow(ctx, `
		SELECT p.id, p.group_id, p.thumb_url, p.preview_url, p.original_key, p.status, p.created_at
		FROM photos p JOIN groups g ON g.id = p.group_id
		WHERE p.id = $1 AND g.owner_id = $2
	`, id, userID))
}

func (s *PhotoStore) ListPhotosByGroup(ctx context.Context, groupID string) ([]Photo, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, group_id, thumb_url, preview_url, original_key, status, created_at
		FROM photos
		WHERE group_id = $1
		ORDER BY created_at DESC, id DESC
	`, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group photos: %w", err)
	}
	defer rows.Close()

	photos := make([]Photo, 0)
	for rows.Next() {
		var photo Photo
		if err := rows.Scan(&photo.ID, &photo.GroupID, &photo.ThumbURL, &photo.PreviewURL, &photo.OriginalKey, &photo.Status, &photo.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan group photo: %w", err)
		}
		photos = append(photos, photo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate group photos: %w", err)
	}
	return photos, nil
}

func (s *PhotoStore) ReplaceFaceEmbeddings(ctx context.Context, photoID, groupID string, faces []FaceEmbedding) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin face embedding transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var photoExists bool
	if err := tx.QueryRow(ctx, `SELECT TRUE FROM photos WHERE id = $1 AND group_id = $2 FOR UPDATE`, photoID, groupID).Scan(&photoExists); err != nil {
		return fmt.Errorf("lock photo for face embeddings: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM face_embeddings WHERE photo_id = $1`, photoID); err != nil {
		return fmt.Errorf("replace face embeddings: delete existing rows: %w", err)
	}
	for _, face := range faces {
		if len(face.BBox) != 4 || len(face.Embedding) != 512 {
			return fmt.Errorf("invalid face embedding shape: bbox=%d embedding=%d", len(face.BBox), len(face.Embedding))
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO face_embeddings (photo_id, group_id, bbox, embedding)
			VALUES ($1, $2, $3, $4)
		`, photoID, groupID, face.BBox, pgvector.NewVector(face.Embedding))
		if err != nil {
			return fmt.Errorf("insert face embedding: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit face embeddings: %w", err)
	}
	return nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scanPhoto(row rowScanner) (*Photo, error) {
	var photo Photo
	err := row.Scan(&photo.ID, &photo.GroupID, &photo.ThumbURL, &photo.PreviewURL, &photo.OriginalKey, &photo.Status, &photo.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("fetch photo: %w", err)
	}
	return &photo, nil
}

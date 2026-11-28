package store

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
)

var ErrPhotoNotFound = errors.New("photo not found")

const (
	PhotoStatusPending     = "pending"
	PhotoStatusProcessing  = "processing"
	PhotoStatusProcessed   = "processed"
	PhotoStatusFailed      = "failed"
	PhotoStatusUnprocessed = "unprocessed"
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

type PhotoStore struct {
	Pool  *pgxpool.Pool
	Cache *MemoryCache
}

type FaceEmbedding struct {
	BBox      []int32
	Embedding []float32
}

type FaceGroup struct {
	ID         string    `json:"id"`
	Label      string    `json:"label"`
	PhotoCount int       `json:"photo_count"`
	PreviewURL string    `json:"preview_url"`
	BBox       []int32   `json:"bbox"`
	CreatedAt  time.Time `json:"created_at"`
}

type FaceGroupOverview struct {
	FaceGroups         []FaceGroup `json:"face_groups"`
	PhotoCount         int         `json:"photo_count"`
	NoFaceCount        int         `json:"no_face_count"`
	UnprocessedCount   int         `json:"unprocessed_count"`
	PendingPhotoIDs    []string    `json:"pending_photo_ids"`
	ProcessingPhotoIDs []string    `json:"processing_photo_ids"`
	FailedPhotoIDs     []string    `json:"failed_photo_ids"`
}

func (s *PhotoStore) CreatePhoto(ctx context.Context, photo Photo) (*Photo, error) {
	query := `
	INSERT INTO photos (id, group_id, thumb_url, preview_url, original_key, status)
	VALUES ($1, $2, $3, $4, $5, 'pending')
	ON CONFLICT (id) DO UPDATE SET
		thumb_url = EXCLUDED.thumb_url,
		preview_url = EXCLUDED.preview_url,
		original_key = EXCLUDED.original_key,
		status = 'pending'
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
	s.Cache.Clear()
	return &created, nil
}

func (s *PhotoStore) GetPhotoByID(ctx context.Context, id string) (*Photo, error) {
	cacheKey := "photos:by-id:" + id
	var cached Photo
	if s.Cache.Get(cacheKey, &cached) {
		return &cached, nil
	}
	photo, err := scanPhoto(s.Pool.QueryRow(ctx, `SELECT id, group_id, thumb_url, preview_url, original_key, status, created_at FROM photos WHERE id = $1`, id))
	if err != nil {
		return nil, err
	}
	s.Cache.Set(cacheKey, photo)
	return photo, nil
}

func (s *PhotoStore) GetPhotoByIDForUser(ctx context.Context, id, userID string) (*Photo, error) {
	return scanPhoto(s.Pool.QueryRow(ctx, `
		SELECT p.id, p.group_id, p.thumb_url, p.preview_url, p.original_key, p.status, p.created_at
		FROM photos p JOIN groups g ON g.id = p.group_id
		WHERE p.id = $1 AND g.owner_id = $2
	`, id, userID))
}

func (s *PhotoStore) GetPhotoByIDForMember(ctx context.Context, id, userID string) (*Photo, error) {
	return scanPhoto(s.Pool.QueryRow(ctx, `
		SELECT p.id, p.group_id, p.thumb_url, p.preview_url, p.original_key, p.status, p.created_at
		FROM photos p JOIN groups g ON g.id = p.group_id
		WHERE p.id = $1 AND (
			g.owner_id = $2 OR EXISTS (
				SELECT 1 FROM group_members gm WHERE gm.group_id = g.id AND gm.user_id = $2
			)
		)
	`, id, userID))
}

func (s *PhotoStore) ListPhotosByGroup(ctx context.Context, groupID string) ([]Photo, error) {
	return s.cachedListPhotos(ctx, "photos:group:"+groupID, `
		SELECT id, group_id, thumb_url, preview_url, original_key, status, created_at
		FROM photos
		WHERE group_id = $1
		ORDER BY created_at DESC, id DESC
	`, groupID)
}

// ListPhotosByGroupFresh bypasses the read cache for destructive cleanup paths.
func (s *PhotoStore) ListPhotosByGroupFresh(ctx context.Context, groupID string) ([]Photo, error) {
	return s.listPhotos(ctx, `
		SELECT id, group_id, thumb_url, preview_url, original_key, status, created_at
		FROM photos
		WHERE group_id = $1
		ORDER BY created_at DESC, id DESC
	`, groupID)
}

func (s *PhotoStore) cachedListPhotos(ctx context.Context, cacheKey, query string, args ...any) ([]Photo, error) {
	var cached []Photo
	if s.Cache.Get(cacheKey, &cached) {
		return cached, nil
	}
	photos, err := s.listPhotos(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	s.Cache.Set(cacheKey, photos)
	return photos, nil
}

func (s *PhotoStore) SetPhotoStatus(ctx context.Context, photoID, status string) error {
	result, err := s.Pool.Exec(ctx, `UPDATE photos SET status = $2 WHERE id = $1`, photoID, status)
	if err != nil {
		return fmt.Errorf("update photo status: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrPhotoNotFound
	}
	s.Cache.Clear()
	return nil
}

func (s *PhotoStore) DeletePhotoForUser(ctx context.Context, photoID, userID string) (bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin photo deletion: %w", err)
	}
	defer tx.Rollback(ctx)

	var groupID string
	err = tx.QueryRow(ctx, `
		DELETE FROM photos p
		USING groups g
		WHERE p.id = $1 AND p.group_id = g.id AND g.owner_id = $2
		RETURNING p.group_id
	`, photoID, userID).Scan(&groupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("delete photo: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM face_clusters fc
		WHERE fc.group_id = $1
			AND NOT EXISTS (SELECT 1 FROM face_embeddings fe WHERE fe.cluster_id = fc.id)
	`, groupID); err != nil {
		return false, fmt.Errorf("remove empty face clusters: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit photo deletion: %w", err)
	}
	s.Cache.Clear()
	return true, nil
}

func (s *PhotoStore) FaceGroupOverview(ctx context.Context, groupID string) (*FaceGroupOverview, error) {
	cacheKey := "photos:overview:" + groupID
	var cached FaceGroupOverview
	if s.Cache.Get(cacheKey, &cached) {
		return &cached, nil
	}
	overview := &FaceGroupOverview{
		FaceGroups:         make([]FaceGroup, 0),
		PendingPhotoIDs:    make([]string, 0),
		ProcessingPhotoIDs: make([]string, 0),
		FailedPhotoIDs:     make([]string, 0),
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT fc.id, fc.created_at, COUNT(DISTINCT fe.photo_id), representative.preview_url, representative.bbox
		FROM face_clusters fc
		JOIN face_embeddings fe ON fe.cluster_id = fc.id
		JOIN LATERAL (
			SELECT p.preview_url, fe2.bbox
			FROM face_embeddings fe2
			JOIN photos p ON p.id = fe2.photo_id
			WHERE fe2.cluster_id = fc.id
			ORDER BY p.created_at, p.id, fe2.id
			LIMIT 1
		) representative ON TRUE
		WHERE fc.group_id = $1
		GROUP BY fc.id, fc.created_at, representative.preview_url, representative.bbox
		ORDER BY fc.created_at, fc.id
	`, groupID)
	if err != nil {
		return nil, fmt.Errorf("list face groups: %w", err)
	}
	for rows.Next() {
		var faceGroup FaceGroup
		if err := rows.Scan(&faceGroup.ID, &faceGroup.CreatedAt, &faceGroup.PhotoCount, &faceGroup.PreviewURL, &faceGroup.BBox); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan face group: %w", err)
		}
		faceGroup.Label = fmt.Sprintf("Face %d", len(overview.FaceGroups)+1)
		overview.FaceGroups = append(overview.FaceGroups, faceGroup)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate face groups: %w", err)
	}
	rows.Close()

	err = s.Pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE p.status = $2 AND NOT EXISTS (
				SELECT 1 FROM face_embeddings fe WHERE fe.photo_id = p.id
			)),
			COUNT(*) FILTER (WHERE p.status = $3)
		FROM photos p
		WHERE p.group_id = $1
	`, groupID, PhotoStatusProcessed, PhotoStatusUnprocessed).Scan(&overview.NoFaceCount, &overview.UnprocessedCount)
	if err != nil {
		return nil, fmt.Errorf("count unclassified photos: %w", err)
	}
	if err := s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM photos WHERE group_id = $1`, groupID).Scan(&overview.PhotoCount); err != nil {
		return nil, fmt.Errorf("count group photos: %w", err)
	}

	statusRows, err := s.Pool.Query(ctx, `
		SELECT id, status FROM photos
		WHERE group_id = $1 AND status IN ($2, $3, $4)
	`, groupID, PhotoStatusPending, PhotoStatusProcessing, PhotoStatusFailed)
	if err != nil {
		return nil, fmt.Errorf("list photo processing states: %w", err)
	}
	defer statusRows.Close()
	for statusRows.Next() {
		var id, status string
		if err := statusRows.Scan(&id, &status); err != nil {
			return nil, fmt.Errorf("scan photo processing state: %w", err)
		}
		switch status {
		case PhotoStatusPending:
			overview.PendingPhotoIDs = append(overview.PendingPhotoIDs, id)
		case PhotoStatusProcessing:
			overview.ProcessingPhotoIDs = append(overview.ProcessingPhotoIDs, id)
		case PhotoStatusFailed:
			overview.FailedPhotoIDs = append(overview.FailedPhotoIDs, id)
		}
	}
	if err := statusRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate photo processing states: %w", err)
	}
	s.Cache.Set(cacheKey, overview)
	return overview, nil
}

func (s *PhotoStore) ListPhotosByFaceCluster(ctx context.Context, groupID, clusterID string) ([]Photo, error) {
	return s.cachedListPhotos(ctx, "photos:face-cluster:"+groupID+":"+clusterID, `
		SELECT DISTINCT p.id, p.group_id, p.thumb_url, p.preview_url, p.original_key, p.status, p.created_at
		FROM photos p
		JOIN face_embeddings fe ON fe.photo_id = p.id
		JOIN face_clusters fc ON fc.id = fe.cluster_id
		WHERE p.group_id = $1 AND fc.group_id = $1 AND fc.id = $2
		ORDER BY p.created_at DESC, p.id DESC
	`, groupID, clusterID)
}

func (s *PhotoStore) ListNoFacePhotos(ctx context.Context, groupID string) ([]Photo, error) {
	return s.cachedListPhotos(ctx, "photos:no-face:"+groupID, `
		SELECT p.id, p.group_id, p.thumb_url, p.preview_url, p.original_key, p.status, p.created_at
		FROM photos p
		WHERE p.group_id = $1 AND p.status = $2
			AND NOT EXISTS (SELECT 1 FROM face_embeddings fe WHERE fe.photo_id = p.id)
		ORDER BY p.created_at DESC, p.id DESC
	`, groupID, PhotoStatusProcessed)
}

func (s *PhotoStore) ListUnprocessedPhotos(ctx context.Context, groupID string) ([]Photo, error) {
	return s.cachedListPhotos(ctx, "photos:unprocessed:"+groupID, `
		SELECT id, group_id, thumb_url, preview_url, original_key, status, created_at
		FROM photos WHERE group_id = $1 AND status = $2
		ORDER BY created_at DESC, id DESC
	`, groupID, PhotoStatusUnprocessed)
}

func (s *PhotoStore) listPhotos(ctx context.Context, query string, args ...any) ([]Photo, error) {
	rows, err := s.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list photos: %w", err)
	}
	defer rows.Close()

	photos := make([]Photo, 0)
	for rows.Next() {
		var photo Photo
		if err := rows.Scan(&photo.ID, &photo.GroupID, &photo.ThumbURL, &photo.PreviewURL, &photo.OriginalKey, &photo.Status, &photo.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan photo: %w", err)
		}
		photos = append(photos, photo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate photos: %w", err)
	}
	return photos, nil
}

func (s *PhotoStore) ReplaceFaceEmbeddings(ctx context.Context, photoID, groupID string, faces []FaceEmbedding, threshold float64) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin face embedding transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var photoExists bool
	if err := tx.QueryRow(ctx, `SELECT TRUE FROM photos WHERE id = $1 AND group_id = $2 FOR UPDATE`, photoID, groupID).Scan(&photoExists); err != nil {
		return fmt.Errorf("lock photo for face embeddings: %w", err)
	}
	// Serialize cluster assignment within a group so concurrent uploads see each
	// other's embeddings and do not create duplicate clusters for the same face.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, groupID); err != nil {
		return fmt.Errorf("lock face cluster assignment: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM face_embeddings WHERE photo_id = $1`, photoID); err != nil {
		return fmt.Errorf("replace face embeddings: delete existing rows: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM face_clusters fc
		WHERE fc.group_id = $1
			AND NOT EXISTS (SELECT 1 FROM face_embeddings fe WHERE fe.cluster_id = fc.id)
	`, groupID); err != nil {
		return fmt.Errorf("remove empty face clusters: %w", err)
	}
	for faceIndex, face := range faces {
		if len(face.BBox) != 4 || len(face.Embedding) != 512 {
			return fmt.Errorf("invalid face embedding shape: bbox=%d embedding=%d", len(face.BBox), len(face.Embedding))
		}
		vector := pgvector.NewVector(face.Embedding)
		var clusterID string
		var similarity float64
		err := tx.QueryRow(ctx, `
			SELECT candidate.cluster_id, 1 - (candidate.embedding <=> $1) AS similarity
			FROM face_embeddings candidate
			WHERE candidate.group_id = $2
				AND candidate.cluster_id IS NOT NULL
				AND NOT EXISTS (
					SELECT 1
					FROM face_embeddings same_photo
					WHERE same_photo.cluster_id = candidate.cluster_id
						AND same_photo.photo_id = $3
				)
			ORDER BY similarity DESC
			LIMIT 1
		`, vector, groupID, photoID).Scan(&clusterID, &similarity)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("find matching face cluster: %w", err)
		}
		if errors.Is(err, pgx.ErrNoRows) || similarity < threshold {
			if errors.Is(err, pgx.ErrNoRows) {
				log.Printf("[FaceMatcher] new cluster: group_id=%s photo_id=%s face_index=%d nearest_similarity=none threshold=%.3f", groupID, photoID, faceIndex, threshold)
			} else {
				log.Printf("[FaceMatcher] new cluster: group_id=%s photo_id=%s face_index=%d nearest_similarity=%.4f threshold=%.3f", groupID, photoID, faceIndex, similarity, threshold)
			}
			if err := tx.QueryRow(ctx, `INSERT INTO face_clusters (group_id) VALUES ($1) RETURNING id`, groupID).Scan(&clusterID); err != nil {
				return fmt.Errorf("create face cluster: %w", err)
			}
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO face_embeddings (photo_id, group_id, bbox, embedding, cluster_id)
			VALUES ($1, $2, $3, $4, $5)
		`, photoID, groupID, face.BBox, vector, clusterID)
		if err != nil {
			return fmt.Errorf("insert face embedding: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE photos SET status = $2 WHERE id = $1`, photoID, PhotoStatusProcessed); err != nil {
		return fmt.Errorf("mark photo processed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit face embeddings: %w", err)
	}
	s.Cache.Clear()
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

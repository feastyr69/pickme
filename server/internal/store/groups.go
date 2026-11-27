package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Group struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type GroupStore struct {
	Pool *pgxpool.Pool
}

func (s *GroupStore) CreateGroup(ctx context.Context, ownerID, name string) (*Group, error) {

	query := `
	INSERT INTO groups (owner_id, name)
	VALUES ($1, $2)
	RETURNING id, name, created_at
	`

	var g Group
	err := s.Pool.QueryRow(ctx, query, ownerID, name).Scan(&g.ID, &g.Name, &g.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create group: %w", err)
	}

	return &g, nil
}

func (s *GroupStore) ListGroups(ctx context.Context, ownerID string) ([]Group, error) {
	query := `
	SELECT id, name, created_at
	FROM groups
	WHERE owner_id = $1
	ORDER BY created_at DESC
	`

	rows, err := s.Pool.Query(ctx, query, ownerID)
	if err != nil {
		return nil, fmt.Errorf("failed to list groups: %w", err)
	}
	defer rows.Close()

	groups := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan group: %w", err)
		}
		groups = append(groups, g)
	}

	return groups, rows.Err()
}

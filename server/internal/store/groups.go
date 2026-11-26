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

func (s *GroupStore) CreateGroup(ctx context.Context, name string) (*Group, error) {

	query := `
	INSERT INTO groups (name)
	VALUES ($1)
	RETURNING id, name, created_at
	`

	var g Group
	err := s.Pool.QueryRow(ctx, query, name).Scan(&g.ID, &g.Name, &g.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create group: %w", err)
	}

	return &g, nil
}

func (s *GroupStore) ListGroups(ctx context.Context) ([]Group, error) {
	query := `
	SELECT id, name, created_at
	FROM groups
	ORDER BY created_at DESC
	`

	rows, err := s.Pool.Query(ctx, query)
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

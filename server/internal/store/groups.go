package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Group struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	IsOwner   bool      `json:"is_owner"`
}

type GroupMember struct {
	UserID  string `json:"user_id"`
	Email   string `json:"email"`
	IsOwner bool   `json:"is_owner"`
}

type GroupInvitation struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type InvitationDetails struct {
	GroupID   string    `json:"group_id"`
	GroupName string    `json:"group_name"`
	ExpiresAt time.Time `json:"expires_at"`
}

type GroupStore struct {
	Pool *pgxpool.Pool
}

var ErrActiveInvitationExists = errors.New("an active invitation already exists for this group")

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
	g.IsOwner = true

	return &g, nil
}

func (s *GroupStore) ListGroups(ctx context.Context, ownerID string) ([]Group, error) {
	query := `
	SELECT g.id, g.name, g.created_at, (g.owner_id = $1) AS is_owner
	FROM groups g
	WHERE g.owner_id = $1
	   OR EXISTS (
		SELECT 1 FROM group_members gm WHERE gm.group_id = g.id AND gm.user_id = $1
	   )
	ORDER BY g.created_at DESC
	`

	rows, err := s.Pool.Query(ctx, query, ownerID)
	if err != nil {
		return nil, fmt.Errorf("failed to list groups: %w", err)
	}
	defer rows.Close()

	groups := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.CreatedAt, &g.IsOwner); err != nil {
			return nil, fmt.Errorf("failed to scan group: %w", err)
		}
		groups = append(groups, g)
	}

	return groups, rows.Err()
}

func (s *GroupStore) UserOwnsGroup(ctx context.Context, ownerID, groupID string) (bool, error) {
	var exists bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM groups WHERE id = $1 AND owner_id = $2)`, groupID, ownerID).Scan(&exists)
	return exists, err
}

func (s *GroupStore) UserCanAccessGroup(ctx context.Context, userID, groupID string) (bool, error) {
	var allowed bool
	err := s.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM groups g
			WHERE g.id = $1 AND (
				g.owner_id = $2 OR EXISTS (
					SELECT 1 FROM group_members gm
					WHERE gm.group_id = g.id AND gm.user_id = $2
				)
			)
		)
	`, groupID, userID).Scan(&allowed)
	return allowed, err
}

func (s *GroupStore) ListGroupMembers(ctx context.Context, groupID string) ([]GroupMember, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT u.id, COALESCE(u.email, ''), TRUE
		FROM groups g JOIN auth.users u ON u.id = g.owner_id
		WHERE g.id = $1
		UNION ALL
		SELECT u.id, COALESCE(u.email, ''), FALSE
		FROM group_members gm JOIN auth.users u ON u.id = gm.user_id
		WHERE gm.group_id = $1
		ORDER BY 3 DESC, 2
	`, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}
	defer rows.Close()

	members := make([]GroupMember, 0)
	for rows.Next() {
		var member GroupMember
		if err := rows.Scan(&member.UserID, &member.Email, &member.IsOwner); err != nil {
			return nil, fmt.Errorf("scan group member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate group members: %w", err)
	}
	return members, nil
}

func (s *GroupStore) RemoveGroupMember(ctx context.Context, groupID, ownerID, memberID string) (bool, error) {
	result, err := s.Pool.Exec(ctx, `
		DELETE FROM group_members gm
		USING groups g
		WHERE gm.group_id = $1 AND gm.user_id = $2
			AND g.id = gm.group_id AND g.owner_id = $3
	`, groupID, memberID, ownerID)
	if err != nil {
		return false, fmt.Errorf("remove group member: %w", err)
	}
	return result.RowsAffected() > 0, nil
}

func (s *GroupStore) CreateGroupInvitation(ctx context.Context, groupID, ownerID string, tokenHash []byte, expiresAt time.Time) (*GroupInvitation, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin group invitation: %w", err)
	}
	defer tx.Rollback(ctx)

	var groupExists bool
	err = tx.QueryRow(ctx, `
		SELECT TRUE FROM groups WHERE id = $1 AND owner_id = $2 FOR UPDATE
	`, groupID, ownerID).Scan(&groupExists)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pgx.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("lock group for invitation: %w", err)
	}
	var activeInvitation bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM group_invitations
			WHERE group_id = $1 AND expires_at > now()
		)
	`, groupID).Scan(&activeInvitation); err != nil {
		return nil, fmt.Errorf("check active group invitation: %w", err)
	}
	if activeInvitation {
		return nil, ErrActiveInvitationExists
	}

	var invitation GroupInvitation
	err = tx.QueryRow(ctx, `
		INSERT INTO group_invitations (group_id, invited_by, token_hash, expires_at)
		SELECT g.id, g.owner_id, $3, $4
		FROM groups g
		WHERE g.id = $1 AND g.owner_id = $2
		RETURNING id, created_at, expires_at
	`, groupID, ownerID, tokenHash, expiresAt).Scan(&invitation.ID, &invitation.CreatedAt, &invitation.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("create group invitation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit group invitation: %w", err)
	}
	return &invitation, nil
}

func (s *GroupStore) ListGroupInvitations(ctx context.Context, groupID string) ([]GroupInvitation, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, created_at, expires_at
		FROM group_invitations
		WHERE group_id = $1 AND expires_at > now()
		ORDER BY created_at DESC
	`, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group invitations: %w", err)
	}
	defer rows.Close()

	invitations := make([]GroupInvitation, 0)
	for rows.Next() {
		var invitation GroupInvitation
		if err := rows.Scan(&invitation.ID, &invitation.CreatedAt, &invitation.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan group invitation: %w", err)
		}
		invitations = append(invitations, invitation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate group invitations: %w", err)
	}
	return invitations, nil
}

func (s *GroupStore) GetInvitationDetails(ctx context.Context, tokenHash []byte) (*InvitationDetails, error) {
	var details InvitationDetails
	err := s.Pool.QueryRow(ctx, `
		SELECT g.id, g.name, gi.expires_at
		FROM group_invitations gi JOIN groups g ON g.id = gi.group_id
		WHERE gi.token_hash = $1 AND gi.expires_at > now()
	`, tokenHash).Scan(&details.GroupID, &details.GroupName, &details.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &details, nil
}

func (s *GroupStore) AcceptGroupInvitation(ctx context.Context, tokenHash []byte, userID string) (string, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin invitation acceptance: %w", err)
	}
	defer tx.Rollback(ctx)

	var invitationID, groupID, ownerID string
	err = tx.QueryRow(ctx, `
		SELECT gi.id, g.id, g.owner_id
		FROM group_invitations gi JOIN groups g ON g.id = gi.group_id
		WHERE gi.token_hash = $1 AND gi.expires_at > now()
		FOR UPDATE OF gi
	`, tokenHash).Scan(&invitationID, &groupID, &ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", pgx.ErrNoRows
	}
	if err != nil {
		return "", fmt.Errorf("fetch group invitation: %w", err)
	}
	if ownerID != userID {
		if _, err := tx.Exec(ctx, `
			INSERT INTO group_members (group_id, user_id)
			VALUES ($1, $2) ON CONFLICT (group_id, user_id) DO NOTHING
		`, groupID, userID); err != nil {
			return "", fmt.Errorf("add group member: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit invitation acceptance: %w", err)
	}
	return groupID, nil
}

func (s *GroupStore) DeleteGroupForUser(ctx context.Context, ownerID, groupID string) (bool, error) {
	result, err := s.Pool.Exec(ctx, `DELETE FROM groups WHERE id = $1 AND owner_id = $2`, groupID, ownerID)
	if err != nil {
		return false, fmt.Errorf("delete group: %w", err)
	}
	return result.RowsAffected() > 0, nil
}

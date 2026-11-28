package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/feastyr69/pickme/server/internal/middleware"
	"github.com/feastyr69/pickme/server/internal/storage"
	"github.com/feastyr69/pickme/server/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const groupInviteLifetime = 7 * 24 * time.Hour

type GroupHandler struct {
	Store   *store.GroupStore
	Photos  *store.PhotoStore
	Storage *storage.DualStorage
}

type createGroupRequest struct {
	Name string `json:"name"`
}

func (h *GroupHandler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB cap

	var req createGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	group, err := h.Store.CreateGroup(r.Context(), userID, req.Name)
	if err != nil {
		http.Error(w, "Failed to create group", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(group)
}

func (h *GroupHandler) ListGroups(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	groups, err := h.Store.ListGroups(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to fetch groups", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(groups)
}

func (h *GroupHandler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	groupUUID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid group ID", http.StatusBadRequest)
		return
	}
	groupID := groupUUID.String()

	owned, err := h.Store.UserOwnsGroup(r.Context(), userID, groupID)
	if err != nil {
		http.Error(w, "Failed to verify group", http.StatusInternalServerError)
		return
	}
	if !owned {
		http.NotFound(w, r)
		return
	}

	photos, err := h.Photos.ListPhotosByGroup(r.Context(), groupID)
	if err != nil {
		http.Error(w, "Failed to list group photos for cleanup", http.StatusInternalServerError)
		return
	}
	for _, photo := range photos {
		if !h.Storage.ValidOriginalKey(groupID, photo.ID, photo.OriginalKey) {
			http.Error(w, "A photo has invalid storage metadata; group was not deleted", http.StatusInternalServerError)
			return
		}
		if err := h.Storage.DeletePhotoObjects(r.Context(), groupID, photo.ID, photo.OriginalKey); err != nil {
			http.Error(w, "Could not remove all group photos from storage; retry group deletion", http.StatusBadGateway)
			return
		}
	}

	deleted, err := h.Store.DeleteGroupForUser(r.Context(), userID, groupID)
	if err != nil {
		http.Error(w, "Failed to delete group", http.StatusInternalServerError)
		return
	}
	if !deleted {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *GroupHandler) CreateGroupInvitation(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	owned, err := h.Store.UserOwnsGroup(r.Context(), userID, groupID)
	if err != nil {
		http.Error(w, "Failed to verify group", http.StatusInternalServerError)
		return
	}
	if !owned {
		http.NotFound(w, r)
		return
	}

	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		http.Error(w, "Failed to create invitation", http.StatusInternalServerError)
		return
	}
	tokenHash := sha256.Sum256(rawToken)
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	expiresAt := time.Now().UTC().Add(groupInviteLifetime)
	invitation, created, err := h.Store.GetOrCreateGroupInvitation(
		r.Context(), groupID, userID, tokenHash[:], token, expiresAt,
	)
	if errors.Is(err, store.ErrLegacyInvitationTokenUnavailable) {
		http.Error(w, fmt.Sprintf(
			"The active invite link was created before copy support and remains valid until %s. Try again after it expires.",
			invitation.ExpiresAt.UTC().Format(time.RFC3339),
		), http.StatusConflict)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Failed to retrieve invitation", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{
		"id":         invitation.ID,
		"token":      invitation.Token,
		"expires_at": invitation.ExpiresAt,
	})
}

func (h *GroupHandler) ListGroupMembers(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	owned, err := h.Store.UserOwnsGroup(r.Context(), userID, groupID)
	if err != nil {
		http.Error(w, "Failed to verify group", http.StatusInternalServerError)
		return
	}
	if !owned {
		http.NotFound(w, r)
		return
	}
	members, err := h.Store.ListGroupMembers(r.Context(), groupID)
	if err != nil {
		http.Error(w, "Failed to fetch group members", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, members)
}

func (h *GroupHandler) RemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	memberID, err := uuid.Parse(r.PathValue("memberID"))
	if err != nil {
		http.Error(w, "Invalid member ID", http.StatusBadRequest)
		return
	}
	removed, err := h.Store.RemoveGroupMember(r.Context(), groupID, userID, memberID.String())
	if err != nil {
		http.Error(w, "Failed to remove group member", http.StatusInternalServerError)
		return
	}
	if !removed {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *GroupHandler) GetInvitationDetails(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.UserIDFromContext(r.Context()); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	tokenHash, ok := invitationTokenHash(r.PathValue("token"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	details, err := h.Store.GetInvitationDetails(r.Context(), tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Failed to fetch invitation", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, details)
}

func (h *GroupHandler) AcceptInvitation(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	tokenHash, ok := invitationTokenHash(r.PathValue("token"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	groupID, err := h.Store.AcceptGroupInvitation(r.Context(), tokenHash, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Failed to accept invitation", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"group_id": groupID})
}

func parseGroupID(w http.ResponseWriter, r *http.Request) (string, bool) {
	groupID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid group ID", http.StatusBadRequest)
		return "", false
	}
	return groupID.String(), true
}

func invitationTokenHash(token string) ([]byte, bool) {
	rawToken, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(rawToken) != 32 {
		return nil, false
	}
	hash := sha256.Sum256(rawToken)
	return hash[:], true
}

package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/feastyr69/pickme/server/internal/middleware"
	"github.com/feastyr69/pickme/server/internal/storage"
	"github.com/feastyr69/pickme/server/internal/store"
	"github.com/google/uuid"
)

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

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/feastyr69/pickme/server/internal/middleware"
	"github.com/feastyr69/pickme/server/internal/services"
	"github.com/feastyr69/pickme/server/internal/storage"
	"github.com/feastyr69/pickme/server/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxOriginalSize = 10 << 20

type UploadHandler struct {
	WorkerPool *services.WorkerPool
	Groups     *store.GroupStore
	Photos     *store.PhotoStore
	Storage    *storage.DualStorage
}

type presignRequest struct {
	GroupID             string `json:"group_id"`
	OriginalContentType string `json:"original_content_type"`
	OriginalSize        int64  `json:"original_size"`
}

type presignResponse struct {
	PhotoID           string `json:"photo_id"`
	ThumbUploadURL    string `json:"thumb_upload_url"`
	PreviewUploadURL  string `json:"preview_upload_url"`
	OriginalUploadURL string `json:"original_upload_url"`
	ThumbURL          string `json:"thumb_url"`
	PreviewURL        string `json:"preview_url"`
	OriginalKey       string `json:"original_key"`
}

type completeRequest struct {
	PhotoID     string `json:"photo_id"`
	GroupID     string `json:"group_id"`
	ThumbURL    string `json:"thumb_url"`
	PreviewURL  string `json:"preview_url"`
	OriginalKey string `json:"original_key"`
}

func (h *UploadHandler) PresignUploads(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req presignRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}
	req.GroupID = strings.TrimSpace(req.GroupID)
	if _, err := uuid.Parse(req.GroupID); err != nil || req.OriginalSize <= 0 || req.OriginalSize > maxOriginalSize {
		http.Error(w, "Invalid group_id or original_size", http.StatusBadRequest)
		return
	}
	ext, ok := originalExtension(req.OriginalContentType)
	if !ok {
		http.Error(w, "Unsupported original_content_type", http.StatusUnsupportedMediaType)
		return
	}
	owned, err := h.Groups.UserOwnsGroup(r.Context(), userID, req.GroupID)
	if err != nil {
		http.Error(w, "Failed to verify group", http.StatusInternalServerError)
		return
	}
	if !owned {
		http.Error(w, "Group not found", http.StatusNotFound)
		return
	}
	photoID := uuid.NewString()
	thumbPut, previewPut, originalPut, thumbURL, previewURL, originalKey, err := h.Storage.PresignImageUploads(r.Context(), req.GroupID, photoID, ext, req.OriginalContentType, req.OriginalSize)
	if err != nil {
		http.Error(w, "Failed to create upload URLs", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, presignResponse{photoID, thumbPut, previewPut, originalPut, thumbURL, previewURL, originalKey})
}

func (h *UploadHandler) CompleteUpload(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req completeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}
	groupID, groupErr := uuid.Parse(strings.TrimSpace(req.GroupID))
	photoID, photoErr := uuid.Parse(strings.TrimSpace(req.PhotoID))
	if groupErr != nil || photoErr != nil {
		http.Error(w, "Invalid group_id or photo_id", http.StatusBadRequest)
		return
	}
	owned, err := h.Groups.UserOwnsGroup(r.Context(), userID, groupID.String())
	if err != nil {
		http.Error(w, "Failed to verify group", http.StatusInternalServerError)
		return
	}
	if !owned {
		http.Error(w, "Group not found", http.StatusNotFound)
		return
	}
	expectedThumb, expectedPreview := h.Storage.PublicImageURLs(groupID.String(), photoID.String())
	if req.ThumbURL != expectedThumb || req.PreviewURL != expectedPreview || !h.Storage.ValidOriginalKey(groupID.String(), photoID.String(), req.OriginalKey) {
		http.Error(w, "Upload metadata does not match photo_id and group_id", http.StatusBadRequest)
		return
	}
	photo, err := h.Photos.CreatePhoto(r.Context(), store.Photo{ID: photoID.String(), GroupID: groupID.String(), ThumbURL: req.ThumbURL, PreviewURL: req.PreviewURL, OriginalKey: req.OriginalKey})
	if err != nil {
		http.Error(w, "Failed to save photo", http.StatusInternalServerError)
		return
	}
	job := services.ImageJob{GroupID: photo.GroupID, PhotoID: photo.ID, PreviewURL: photo.PreviewURL}
	select {
	case h.WorkerPool.JobQueue <- job:
		writeJSON(w, http.StatusAccepted, photo)
	default:
		http.Error(w, "Server is busy. Please try again later.", http.StatusServiceUnavailable)
	}
}

func (h *UploadHandler) DownloadOriginal(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	photoID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid photo ID", http.StatusBadRequest)
		return
	}
	photo, err := h.Photos.GetPhotoByIDForUser(r.Context(), photoID.String(), userID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Failed to fetch photo", http.StatusInternalServerError)
		return
	}
	url, err := h.Storage.PresignOriginalDownload(r.Context(), photo.OriginalKey, photo.ID)
	if err != nil {
		http.Error(w, "Failed to create download URL", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"download_url": url})
}

func originalExtension(contentType string) (string, bool) {
	switch contentType {
	case "image/jpeg":
		return "jpg", true
	case "image/png":
		return "png", true
	case "image/webp":
		return "webp", true
	default:
		return "", false
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

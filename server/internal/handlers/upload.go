package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/feastyr69/pickme/server/internal/services"
)

type UploadHandler struct {
	WorkerPool *services.WorkerPool
}

const maxUploadSize = 10 << 20 // 10 MB

var allowedFileTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

func (h *UploadHandler) HandleUpload(w http.ResponseWriter, r *http.Request) {

	//error control

	if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		http.Error(w, "Invalid content type", http.StatusUnsupportedMediaType)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, "File too large", http.StatusRequestEntityTooLarge)
		return
	}

	groupID := strings.TrimSpace(r.FormValue("group_id"))
	if groupID == "" {
		http.Error(w, "group_id is required", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("photo")
	if err != nil {
		http.Error(w, "Missing or invalid photo field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	header := make([]byte, 512)
	n, err := file.Read(header)
	if err != nil && err != io.EOF {
		http.Error(w, "Failed to read file", http.StatusInternalServerError)
		return
	}

	detectedType := http.DetectContentType(header[:n])
	if !allowedFileTypes[detectedType] {
		http.Error(w, "Invalid file type. Only JPEG, PNG, and WebP are allowed", http.StatusUnsupportedMediaType)
		return
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "Failed to process file", http.StatusInternalServerError)
		return
	}

	//successful upload, send to worker queue

	log.Println("Received file upload request")

	job := services.ImageJob{
		TempPath: "/tmp/uploaded_file.jpg",
		GroupID:  groupID,
		FileType: detectedType,
	}

	select {
	case h.WorkerPool.JobQueue <- job:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"message": "File uploaded successfully",
		})
	default:
		http.Error(w, "Server is busy. Please try again later.", http.StatusServiceUnavailable)
		return
	}

}

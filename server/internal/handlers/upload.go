package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

type ImageJob struct {
	TempPath string
	GroupID  string
}

const maxUploadSize = 10 << 20 // 10 MB

var allowedFileTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

var ImageJobQueue = make(chan ImageJob, 100) //channel to hold jobs

func ImageProcessorWorker(id int, jobs <-chan ImageJob) {
	for job := range jobs {
		fmt.Printf("[Worker %d] Processing image at %s for group %s\n", id, job.TempPath, job.GroupID)
	}
}

func HandleUpload(w http.ResponseWriter, r *http.Request) {

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

	ImageJobQueue <- ImageJob{
		TempPath: "/tmp/uploaded_file.jpg",
		GroupID:  r.FormValue("group_id"),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "File uploaded successfully",
	})
}

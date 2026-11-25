package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

type ImageJob struct {
	TempPath string
	GroupID  string
}

var ImageJobQueue = make(chan ImageJob, 100) //channel to hold jobs

func ImageProcessorWorker(id int, jobs <-chan ImageJob) {
	for job := range jobs {
		fmt.Printf("[Worker %d] Processing image at %s for group %s\n", id, job.TempPath, job.GroupID)
	}
}

func HandleUpload(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(10 << 20) //limits upload size to 10MB

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

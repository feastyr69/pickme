package handlers

import (
	"encoding/json"
	"log"
	"net/http"
)

func HandleUpload(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(10 << 20) //limits upload size to 10MB

	log.Println("Received file upload request", r)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "File uploaded successfully",
	})
}

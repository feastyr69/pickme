package main

import (
	"log"
	"net/http"

	"github.com/feastyr69/pickme/server/internal/handlers"
)

func main() {

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", handlers.HandleHealthCheck)
	mux.HandleFunc("POST /api/upload", handlers.HandleUpload)

	log.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

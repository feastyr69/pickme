package main

import (
	"log"
	"net/http"

	"github.com/feastyr69/pickme/server/internal/handlers"
	"github.com/feastyr69/pickme/server/internal/services"
)

func main() {

	pool := services.NewWorkerPool(100) // Create a worker pool with a queue size of 10
	pool.Start(5)                       // Start 5 workers

	uploadHandler := &handlers.UploadHandler{
		WorkerPool: pool,
	}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", handlers.HandleHealthCheck)
	mux.HandleFunc("POST /api/upload", uploadHandler.HandleUpload)

	log.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

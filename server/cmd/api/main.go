package main

import (
	"log"
	"net/http"

	"github.com/feastyr69/pickme/server/internal/handlers"
	"github.com/feastyr69/pickme/server/internal/services"
	"github.com/feastyr69/pickme/server/internal/store"
	"github.com/joho/godotenv"
)

func main() {

	//db setup

	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	db, err := store.ConnectDB()
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	defer db.Close()
	log.Println("Connected to Supabase PostgreSQL")

	groupStore := &store.GroupStore{Pool: db}
	groupHandler := &handlers.GroupHandler{Store: groupStore}

	//worker setup

	pool := services.NewWorkerPool(100)
	pool.Start(5)

	uploadHandler := &handlers.UploadHandler{
		WorkerPool: pool,
	}

	//http server setup
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", handlers.HandleHealthCheck)
	mux.HandleFunc("POST /api/upload", uploadHandler.HandleUpload)
	mux.HandleFunc("POST /api/groups", groupHandler.CreateGroup)
	mux.HandleFunc("GET /api/groups", groupHandler.ListGroups)

	log.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

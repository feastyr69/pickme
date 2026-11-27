package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/feastyr69/pickme/server/internal/handlers"
	"github.com/feastyr69/pickme/server/internal/middleware"
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
	jwtVerifier, err := middleware.NewSupabaseJWTVerifier(os.Getenv("SUPABASE_URL"))
	if err != nil {
		log.Fatalf("JWT verifier setup failed: %v", err)
	}
	defer jwtVerifier.Close()

	//worker setup

	pool := services.NewWorkerPool(100)
	pool.Start(5)

	uploadHandler := &handlers.UploadHandler{
		WorkerPool: pool,
	}

	//http server setup
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", handlers.HandleHealthCheck)
	mux.Handle("POST /api/upload", jwtVerifier.Middleware(http.HandlerFunc(uploadHandler.HandleUpload)))
	mux.Handle("POST /api/groups", jwtVerifier.Middleware(http.HandlerFunc(groupHandler.CreateGroup)))
	mux.Handle("GET /api/groups", jwtVerifier.Middleware(http.HandlerFunc(groupHandler.ListGroups)))

	server := &http.Server{
		Addr:              ":8080",
		Handler:           middleware.BrowserCORS(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Println("Starting server on :8080")
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

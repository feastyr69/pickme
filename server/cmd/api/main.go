package main

import (
	"context"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/feastyr69/pickme/server/internal/handlers"
	"github.com/feastyr69/pickme/server/internal/middleware"
	"github.com/feastyr69/pickme/server/internal/services"
	"github.com/feastyr69/pickme/server/internal/storage"
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
	photoStore := &store.PhotoStore{Pool: db}
	dualStorage, err := storage.NewDualStorage(context.Background())
	if err != nil {
		log.Fatalf("Storage setup failed: %v", err)
	}
	groupHandler := &handlers.GroupHandler{Store: groupStore, Photos: photoStore, Storage: dualStorage}
	jwtVerifier, err := middleware.NewSupabaseJWTVerifier(os.Getenv("SUPABASE_URL"))
	if err != nil {
		log.Fatalf("JWT verifier setup failed: %v", err)
	}
	defer jwtVerifier.Close()

	//worker setup

	faceExtractor, err := services.NewFaceExtractorClient(os.Getenv("FACE_EXTRACTOR_URL"))
	if err != nil {
		log.Fatalf("Face extractor setup failed: %v", err)
	}
	faceMatchThreshold := 0.55
	if configuredThreshold := strings.TrimSpace(os.Getenv("FACE_MATCH_THRESHOLD")); configuredThreshold != "" {
		faceMatchThreshold, err = strconv.ParseFloat(configuredThreshold, 64)
		if err != nil || math.IsNaN(faceMatchThreshold) || math.IsInf(faceMatchThreshold, 0) || faceMatchThreshold <= 0 || faceMatchThreshold > 1 {
			log.Fatal("FACE_MATCH_THRESHOLD must be greater than 0 and at most 1")
		}
	}
	photoProcessor := &services.PhotoProcessor{Extractor: faceExtractor, Photos: photoStore, Threshold: faceMatchThreshold}
	pool := services.NewWorkerPool(100, photoProcessor)
	pool.Start(5)

	uploadHandler := &handlers.UploadHandler{
		WorkerPool: pool,
		Groups:     groupStore,
		Photos:     photoStore,
		Storage:    dualStorage,
	}

	//http server setup
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", handlers.HandleHealthCheck)
	mux.Handle("POST /api/uploads/presign", jwtVerifier.Middleware(http.HandlerFunc(uploadHandler.PresignUploads)))
	mux.Handle("POST /api/uploads/complete", jwtVerifier.Middleware(http.HandlerFunc(uploadHandler.CompleteUpload)))
	mux.Handle("GET /api/groups/{id}/photos", jwtVerifier.Middleware(http.HandlerFunc(uploadHandler.ListGroupPhotos)))
	mux.Handle("GET /api/groups/{id}/face-groups", jwtVerifier.Middleware(http.HandlerFunc(uploadHandler.ListFaceGroups)))
	mux.Handle("GET /api/groups/{id}/face-groups/{clusterID}/photos", jwtVerifier.Middleware(http.HandlerFunc(uploadHandler.ListFaceClusterPhotos)))
	mux.Handle("GET /api/groups/{id}/no-face-photos", jwtVerifier.Middleware(http.HandlerFunc(uploadHandler.ListNoFacePhotos)))
	mux.Handle("GET /api/groups/{id}/unprocessed-photos", jwtVerifier.Middleware(http.HandlerFunc(uploadHandler.ListUnprocessedPhotos)))
	mux.Handle("GET /api/photos/{id}/download", jwtVerifier.Middleware(http.HandlerFunc(uploadHandler.DownloadOriginal)))
	mux.Handle("POST /api/groups", jwtVerifier.Middleware(http.HandlerFunc(groupHandler.CreateGroup)))
	mux.Handle("GET /api/groups", jwtVerifier.Middleware(http.HandlerFunc(groupHandler.ListGroups)))
	mux.Handle("DELETE /api/groups/{id}", jwtVerifier.Middleware(http.HandlerFunc(groupHandler.DeleteGroup)))

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

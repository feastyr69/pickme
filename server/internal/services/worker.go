package services

import (
	"log"
	"time"
)

type ImageJob struct {
	GroupID  string
	FileType string
}

type WorkerPool struct {
	JobQueue chan ImageJob
}

func NewWorkerPool(queueSize int) *WorkerPool {
	return &WorkerPool{
		JobQueue: make(chan ImageJob, queueSize), // buffered channel to hold jobs
	}
}

func (wp *WorkerPool) worker(id int) {
	for job := range wp.JobQueue {
		log.Printf("[Worker %d] Processing upload for group %s with file type %s\n", id, job.GroupID, job.FileType)

		time.Sleep(2 * time.Second) // Simulate processing time

		log.Printf("[Worker %d] Finished processing upload for group %s\n", id, job.GroupID)

	}
}

func (wp *WorkerPool) Start(numWorkers int) {
	for i := 0; i < numWorkers; i++ {
		go wp.worker(i)
	}
}

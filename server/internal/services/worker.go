package services

import (
	"log"
	"time"
)

type ImageJob struct {
	GroupID    string
	PhotoID    string
	PreviewURL string
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
		log.Printf("[Worker %d] Processing photo %s for group %s from %s\n", id, job.PhotoID, job.GroupID, job.PreviewURL)

		time.Sleep(2 * time.Second) // Simulate processing time

		log.Printf("[Worker %d] Finished processing photo %s\n", id, job.PhotoID)

	}
}

func (wp *WorkerPool) Start(numWorkers int) {
	for i := 0; i < numWorkers; i++ {
		go wp.worker(i)
	}
}

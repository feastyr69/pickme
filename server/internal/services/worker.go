package services

import (
	"context"
	"log"
	"time"
)

type ImageJob struct {
	GroupID    string
	PhotoID    string
	PreviewURL string
}

type ImageProcessor interface {
	Process(context.Context, ImageJob) error
}

type WorkerPool struct {
	JobQueue  chan ImageJob
	processor ImageProcessor
}

func NewWorkerPool(queueSize int, processor ImageProcessor) *WorkerPool {
	return &WorkerPool{
		JobQueue:  make(chan ImageJob, queueSize),
		processor: processor,
	}
}

func (wp *WorkerPool) worker(id int) {
	for job := range wp.JobQueue {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		err := wp.processor.Process(ctx, job)
		cancel()
		if err != nil {
			log.Printf("[Worker %d] Failed to process photo %s: %v", id, job.PhotoID, err)
			continue
		}
		log.Printf("[Worker %d] Processed photo %s", id, job.PhotoID)
	}
}

func (wp *WorkerPool) Start(numWorkers int) {
	for i := 0; i < numWorkers; i++ {
		go wp.worker(i)
	}
}

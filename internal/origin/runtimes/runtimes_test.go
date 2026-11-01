package runtimes

import (
	"net"
	"sync"
	"time"

	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/origin"
)

// WorkerPool implements a bounded worker goroutine pool with an in-memory connection queue.
type WorkerPool struct {
	numWorkers int
	queueSize  int
}

// NewWorkerPool creates a new WorkerPool runtime with the specified worker count and queue capacity.
func NewWorkerPool(numWorkers, queueSize int) *WorkerPool {
	if numWorkers <= 0 {
		numWorkers = 64
	}
	if queueSize <= 0 {
		queueSize = 1000
	}
	return &WorkerPool{
		numWorkers: numWorkers,
		queueSize:  queueSize,
	}
}

// Run executes the bounded worker pool listener and accept loop.
func (p *WorkerPool) Run(listener *net.TCPListener, dispatcher *origin.StaticDispatcher, stopCh <-chan struct{}) error {
	connQueue := make(chan net.Conn, p.queueSize)
	var wg sync.WaitGroup

	// Spawn fixed pool of worker goroutines
	for i := 0; i < p.numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for conn := range connQueue {
				origin.HandleConnection(conn, dispatcher)
			}
		}()
	}

	// Accept loop
	for {
		select {
		case <-stopCh:
			_ = listener.Close()
			close(connQueue)
			wg.Wait()
			return nil
		default:
		}

		// Set 1-second accept deadline to periodically evaluate stopCh
		_ = listener.SetDeadline(time.Now().Add(1 * time.Second))
		conn, err := listener.AcceptTCP()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			// Check if shutdown occurred
			select {
			case <-stopCh:
				_ = listener.Close()
				close(connQueue)
				wg.Wait()
				return nil
			default:
				continue
			}
		}

		// Dispatch connection to the worker queue with backpressure handling
		select {
		case connQueue <- conn:
		case <-stopCh:
			_ = conn.Close()
			_ = listener.Close()
			close(connQueue)
			wg.Wait()
			return nil
		}
	}
}
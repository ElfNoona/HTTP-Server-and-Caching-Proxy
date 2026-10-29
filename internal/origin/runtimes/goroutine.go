package runtimes

import (
	"net"
	"sync"
	"time"

	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/origin"
)

// GoroutineRuntime implements a goroutine-per-connection concurrency model.
type GoroutineRuntime struct {
	wg sync.WaitGroup
}

// NewGoroutineRuntime creates a new GoroutineRuntime instance.
func NewGoroutineRuntime() *GoroutineRuntime {
	return &GoroutineRuntime{}
}

// Run executes the goroutine-per-connection listener and accept loop.
func (r *GoroutineRuntime) Run(listener *net.TCPListener, dispatcher *origin.StaticDispatcher, stopCh <-chan struct{}) error {
	for {
		select {
		case <-stopCh:
			_ = listener.Close()
			r.wg.Wait()
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
				r.wg.Wait()
				return nil
			default:
				continue
			}
		}

		// Spawn a dedicated goroutine for each accepted connection
		r.wg.Add(1)
		go func(c net.Conn) {
			defer r.wg.Done()
			origin.HandleConnection(c, dispatcher)
		}(conn)
	}
}
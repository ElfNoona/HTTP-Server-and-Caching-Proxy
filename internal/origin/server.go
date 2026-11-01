package origin

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/http"
)

// DefaultKeepAliveTimeout specifies the idle read deadline for persistent connections.
const DefaultKeepAliveTimeout = 5 * time.Second

// ConcurrencyRuntime defines the interface for swappable origin server concurrency models.
type ConcurrencyRuntime interface {
	Run(listener *net.TCPListener, dispatcher *StaticDispatcher, stopCh <-chan struct{}) error
}

// HandleConnection processes persistent HTTP/1.1 requests sequentially on a raw TCP connection.
func HandleConnection(conn net.Conn, dispatcher *StaticDispatcher) {
	defer conn.Close()

	acc := http.NewBufferAccumulator(conn)

	// Keep-Alive Request Loop
	for {
		req, err := acc.ReadNextRequest(DefaultKeepAliveTimeout)
		if err != nil {
			// Clean disconnect: client closed connection normally
			if err == io.EOF {
				return
			}

			// Idle timeout on keep-alive connection
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				return
			}
			if errors.Is(err, os.ErrDeadlineExceeded) || strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline exceeded") {
				return
			}

			// HTTP framing or protocol violation error
			if statusErr, ok := err.(*http.StatusError); ok {
				errRes := http.NewErrorResponse(statusErr)
				_, _ = errRes.WriteTo(conn)
				return
			}

			// Unexpected internal error
			errRes := http.NewErrorResponse(http.NewStatusError(http.StatusInternalServerError, "Internal Server Error"))
			_, _ = errRes.WriteTo(conn)
			return
		}

		// Dispatch request to static dispatcher
		res, serveErr := dispatcher.Serve(req)
		if serveErr != nil {
			res = http.NewErrorResponse(serveErr)
		}

		// Honor connection persistence
		if req.Close {
			res.SetHeader("Connection", "close")
		} else {
			res.SetHeader("Connection", "keep-alive")
			res.SetHeader("Keep-Alive", "timeout=5")
		}

		// Write response to client socket
		bytesWritten, writeErr := res.WriteTo(conn)
		if writeErr != nil {
			return
		}

		// Structured concise log output
		fmt.Printf("[ORIGIN] [%d] %s %s (%d bytes)\n", res.StatusCode, req.Method, req.URI, bytesWritten)

		// Terminate connection if client requested close or error response was generated
		if req.Close {
			break
		}
	}
}
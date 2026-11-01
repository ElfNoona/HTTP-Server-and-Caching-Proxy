package tests

import (
	"bytes"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/http"
)

// TestTCPFragmentation validates that BufferAccumulator correctly reassembles
// severely fragmented TCP streams into complete, well-formed HTTP requests.
func TestTCPFragmentation(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})

	bodyContent := "Sample payload to evaluate boundary-less raw TCP fragmentation and accumulator sliding window assembly."
	rawRequest := fmt.Sprintf(
		"POST /api/v1/fragmented?stream=true HTTP/1.1\r\n"+
			"Host: 127.0.0.1:8080\r\n"+
			"User-Agent: FragmentedStreamClient/1.0\r\n"+
			"X-Custom-Header: AlphaBetaGamma\r\n"+
			"Content-Type: text/plain; charset=utf-8\r\n"+
			"Content-Length: %d\r\n"+
			"Connection: keep-alive\r\n"+
			"\r\n"+
			"%s",
		len(bodyContent),
		bodyContent,
	)
	rawBytes := []byte(rawRequest)

	// Stream fragmented chunks (between 3 and 7 bytes each) with small sleep intervals
	chunkSizes := []int{3, 7, 4, 6, 5, 3, 7, 5, 4}
	go func() {
		offset := 0
		step := 0
		for offset < len(rawBytes) {
			size := chunkSizes[step%len(chunkSizes)]
			step++
			end := offset + size
			if end > len(rawBytes) {
				end = len(rawBytes)
			}

			if _, err := clientConn.Write(rawBytes[offset:end]); err != nil {
				return
			}
			offset = end
			time.Sleep(1 * time.Millisecond)
		}
	}()

	acc := http.NewBufferAccumulator(serverConn)
	req, err := acc.ReadNextRequest(5 * time.Second)
	if err != nil {
		t.Fatalf("failed to read fragmented request: %v", err)
	}

	if req.Method != "POST" {
		t.Errorf("expected Method 'POST', got %q", req.Method)
	}
	if req.URI != "/api/v1/fragmented?stream=true" {
		t.Errorf("expected URI '/api/v1/fragmented?stream=true', got %q", req.URI)
	}
	if req.Path != "/api/v1/fragmented" {
		t.Errorf("expected Path '/api/v1/fragmented', got %q", req.Path)
	}
	if req.QueryParams["stream"] != "true" {
		t.Errorf("expected QueryParam stream='true', got %q", req.QueryParams["stream"])
	}
	if req.Headers["x-custom-header"] != "AlphaBetaGamma" {
		t.Errorf("expected header X-Custom-Header='AlphaBetaGamma', got %q", req.Headers["x-custom-header"])
	}
	if req.Headers["user-agent"] != "FragmentedStreamClient/1.0" {
		t.Errorf("expected header User-Agent='FragmentedStreamClient/1.0', got %q", req.Headers["user-agent"])
	}
	if int64(len(req.Body)) != req.ContentLength {
		t.Errorf("content-length mismatch: header=%d, actual body len=%d", req.ContentLength, len(req.Body))
	}
	if !bytes.Equal(req.Body, []byte(bodyContent)) {
		t.Errorf("body content mismatch: expected %q, got %q", bodyContent, string(req.Body))
	}
}


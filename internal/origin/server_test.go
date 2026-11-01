package origin

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/http"
)

func TestHandleConnection_KeepAliveAndClose(t *testing.T) {
	tempDir := t.TempDir()
	content := "Hello Server"
	if err := os.WriteFile(filepath.Join(tempDir, "index.html"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write index.html: %v", err)
	}

	dispatcher, err := NewStaticDispatcher(tempDir)
	if err != nil {
		t.Fatalf("NewStaticDispatcher failed: %v", err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	// Run HandleConnection in a goroutine
	go HandleConnection(serverConn, dispatcher)

	// 1. Send first request with keep-alive
	req1 := "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"
	_, _ = clientConn.Write([]byte(req1))

	acc := http.NewBufferAccumulator(clientConn)
	// Read response 1 headers manually from client socket
	res1Buf := make([]byte, 1024)
	n1, err := clientConn.Read(res1Buf)
	if err != nil {
		t.Fatalf("failed reading response 1: %v", err)
	}
	respStr1 := string(res1Buf[:n1])
	if !strings.Contains(respStr1, "200 OK") {
		t.Errorf("expected 200 OK, got: %s", respStr1)
	}
	if !strings.Contains(respStr1, "Connection: keep-alive") {
		t.Errorf("expected Connection: keep-alive, got: %s", respStr1)
	}
	if !strings.Contains(respStr1, content) {
		t.Errorf("expected body %q in response 1, got: %s", content, respStr1)
	}

	// 2. Send second request on the SAME connection with Connection: close
	req2 := "GET /index.html HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"
	_, _ = clientConn.Write([]byte(req2))

	res2Buf := make([]byte, 1024)
	n2, err := clientConn.Read(res2Buf)
	if err != nil {
		t.Fatalf("failed reading response 2: %v", err)
	}
	respStr2 := string(res2Buf[:n2])
	if !strings.Contains(respStr2, "200 OK") {
		t.Errorf("expected 200 OK, got: %s", respStr2)
	}
	if !strings.Contains(respStr2, "Connection: close") {
		t.Errorf("expected Connection: close, got: %s", respStr2)
	}

	_ = acc
}

func TestHandleConnection_MalformedRequest(t *testing.T) {
	tempDir := t.TempDir()
	dispatcher, err := NewStaticDispatcher(tempDir)
	if err != nil {
		t.Fatalf("NewStaticDispatcher failed: %v", err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	go HandleConnection(serverConn, dispatcher)

	// Send malformed request line
	_, _ = clientConn.Write([]byte("INVALID\r\n\r\n"))

	buf := make([]byte, 1024)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatalf("failed reading error response: %v", err)
	}
	respStr := string(buf[:n])
	if !strings.Contains(respStr, "400 Bad Request") {
		t.Errorf("expected 400 Bad Request, got: %s", respStr)
	}
}

func TestHandleConnection_Timeout(t *testing.T) {
	tempDir := t.TempDir()
	dispatcher, err := NewStaticDispatcher(tempDir)
	if err != nil {
		t.Fatalf("NewStaticDispatcher failed: %v", err)
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	done := make(chan struct{})
	go func() {
		HandleConnection(serverConn, dispatcher)
		close(done)
	}()

	// Do not write anything; close client to trigger EOF/disconnect
	_ = clientConn.Close()

	select {
	case <-done:
		// Clean exit
	case <-time.After(2 * time.Second):
		t.Fatal("HandleConnection did not exit after client disconnect")
	}
}

func Example() {
	fmt.Println("origin package initialized")
	// Output: origin package initialized
}
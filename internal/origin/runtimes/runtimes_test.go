package runtimes

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/origin"
)

func setupTestDispatcher(t *testing.T) *origin.StaticDispatcher {
	tempDir := t.TempDir()
	content := "Worker Pool Body"
	if err := os.WriteFile(filepath.Join(tempDir, "index.html"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write index.html: %v", err)
	}
	dispatcher, err := origin.NewStaticDispatcher(tempDir)
	if err != nil {
		t.Fatalf("failed to create dispatcher: %v", err)
	}
	return dispatcher
}

func TestWorkerPool_RunAndShutdown(t *testing.T) {
	dispatcher := setupTestDispatcher(t)

	laddr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to resolve TCP addr: %v", err)
	}
	listener, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		t.Fatalf("failed to listen on TCP: %v", err)
	}
	defer listener.Close()

	pool := NewWorkerPool(4, 50)
	stopCh := make(chan struct{})
	doneCh := make(chan error, 1)

	go func() {
		doneCh <- pool.Run(listener, dispatcher, stopCh)
	}()

	// Connect client and send request
	serverAddr := listener.Addr().String()
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		t.Fatalf("failed to dial server: %v", err)
	}
	defer conn.Close()

	req := "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("failed to write request: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	resStr := string(buf[:n])
	if !strings.Contains(resStr, "200 OK") || !strings.Contains(resStr, "Worker Pool Body") {
		t.Errorf("unexpected response: %s", resStr)
	}

	// Trigger graceful shutdown
	close(stopCh)
	select {
	case err := <-doneCh:
		if err != nil {
			t.Errorf("WorkerPool.Run returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("WorkerPool failed to shut down in time")
	}
}

func TestGoroutineRuntime_RunAndShutdown(t *testing.T) {
	dispatcher := setupTestDispatcher(t)

	laddr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to resolve TCP addr: %v", err)
	}
	listener, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		t.Fatalf("failed to listen on TCP: %v", err)
	}
	defer listener.Close()

	runtime := NewGoroutineRuntime()
	stopCh := make(chan struct{})
	doneCh := make(chan error, 1)

	go func() {
		doneCh <- runtime.Run(listener, dispatcher, stopCh)
	}()

	// Connect client and send request
	serverAddr := listener.Addr().String()
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		t.Fatalf("failed to dial server: %v", err)
	}
	defer conn.Close()

	req := fmt.Sprintf("GET /index.html HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", serverAddr)
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("failed to write request: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	resStr := string(buf[:n])
	if !strings.Contains(resStr, "200 OK") || !strings.Contains(resStr, "Worker Pool Body") {
		t.Errorf("unexpected response: %s", resStr)
	}

	// Trigger graceful shutdown
	close(stopCh)
	select {
	case err := <-doneCh:
		if err != nil {
			t.Errorf("GoroutineRuntime.Run returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("GoroutineRuntime failed to shut down in time")
	}
}


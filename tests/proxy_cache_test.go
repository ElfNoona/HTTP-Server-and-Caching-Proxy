package tests

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/http"
	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/origin"
	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/proxy"
	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/proxy/cache"
)

// TestProxyEndToEndCache verifies the end-to-end forward caching proxy pipeline:
// cold cache miss, warm cache hit, and conditional 304 revalidation.
func TestProxyEndToEndCache(t *testing.T) {
	// 1. Setup temporary document root with a static asset
	tempDir := t.TempDir()
	testPayload := "Public catalog payload for end-to-end proxy cache verification."
	assetPath := filepath.Join(tempDir, "catalog.txt")
	if err := os.WriteFile(assetPath, []byte(testPayload), 0644); err != nil {
		t.Fatalf("failed to create test asset: %v", err)
	}

	dispatcher, err := origin.NewStaticDispatcher(tempDir)
	if err != nil {
		t.Fatalf("failed to initialize static dispatcher: %v", err)
	}

	// 2. Spin up Origin Server on dynamic port 127.0.0.1:0
	originAddr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to resolve origin address: %v", err)
	}
	originListener, err := net.ListenTCP("tcp", originAddr)
	if err != nil {
		t.Fatalf("failed to bind origin listener: %v", err)
	}
	t.Cleanup(func() { _ = originListener.Close() })

	go func() {
		for {
			conn, acceptErr := originListener.AcceptTCP()
			if acceptErr != nil {
				return
			}
			go origin.HandleConnection(conn, dispatcher)
		}
	}()

	originHost := originListener.Addr().String()

	// 3. Spin up Proxy Server with 1 MB LRU Cache
	proxyAddr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to resolve proxy address: %v", err)
	}
	proxyListener, err := net.ListenTCP("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("failed to bind proxy listener: %v", err)
	}
	t.Cleanup(func() { _ = proxyListener.Close() })

	lru := cache.NewLRUCache(1024 * 1024)
	proxyServer := proxy.NewProxyServer(proxyListener, originHost, lru)
	go proxyServer.Start(4)
	t.Cleanup(func() { proxyServer.Stop() })

	proxyHost := proxyListener.Addr().String()

	// --- Request 1: Cold Cache (Expect 200 OK, X-Cache: MISS) ---
	conn1, err := net.Dial("tcp", proxyHost)
	if err != nil {
		t.Fatalf("failed to connect to proxy for Request 1: %v", err)
	}
	reqStr1 := fmt.Sprintf("GET /catalog.txt HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", originHost)
	if _, err := conn1.Write([]byte(reqStr1)); err != nil {
		t.Fatalf("failed to send Request 1: %v", err)
	}

	acc1 := http.NewBufferAccumulator(conn1)
	res1, err := acc1.ReadNextResponse(5 * time.Second)
	_ = conn1.Close()
	if err != nil {
		t.Fatalf("failed to read response for Request 1: %v", err)
	}

	if res1.StatusCode != http.StatusOK {
		t.Errorf("Request 1: expected status 200, got %d", res1.StatusCode)
	}
	if xCache := res1.GetHeader("X-Cache"); xCache != "MISS" {
		t.Errorf("Request 1: expected X-Cache: MISS, got %q", xCache)
	}
	if string(res1.Body) != testPayload {
		t.Errorf("Request 1: expected body %q, got %q", testPayload, string(res1.Body))
	}

	etag := res1.GetHeader("ETag")
	if etag == "" {
		t.Fatalf("Request 1: expected valid ETag header in response")
	}

	// --- Request 2: Warm Cache (Expect 200 OK, X-Cache: HIT) ---
	conn2, err := net.Dial("tcp", proxyHost)
	if err != nil {
		t.Fatalf("failed to connect to proxy for Request 2: %v", err)
	}
	reqStr2 := fmt.Sprintf("GET /catalog.txt HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", originHost)
	if _, err := conn2.Write([]byte(reqStr2)); err != nil {
		t.Fatalf("failed to send Request 2: %v", err)
	}

	acc2 := http.NewBufferAccumulator(conn2)
	res2, err := acc2.ReadNextResponse(5 * time.Second)
	_ = conn2.Close()
	if err != nil {
		t.Fatalf("failed to read response for Request 2: %v", err)
	}

	if res2.StatusCode != http.StatusOK {
		t.Errorf("Request 2: expected status 200, got %d", res2.StatusCode)
	}
	if xCache := res2.GetHeader("X-Cache"); xCache != "HIT" {
		t.Errorf("Request 2: expected X-Cache: HIT, got %q", xCache)
	}
	if string(res2.Body) != testPayload {
		t.Errorf("Request 2: expected cached body %q, got %q", testPayload, string(res2.Body))
	}

	// --- Request 3: Conditional Revalidation (Expect 304 Not Modified, zero body) ---
	conn3, err := net.Dial("tcp", proxyHost)
	if err != nil {
		t.Fatalf("failed to connect to proxy for Request 3: %v", err)
	}
	reqStr3 := fmt.Sprintf("GET /catalog.txt HTTP/1.1\r\nHost: %s\r\nIf-None-Match: %s\r\nConnection: close\r\n\r\n", originHost, etag)
	if _, err := conn3.Write([]byte(reqStr3)); err != nil {
		t.Fatalf("failed to send Request 3: %v", err)
	}

	acc3 := http.NewBufferAccumulator(conn3)
	res3, err := acc3.ReadNextResponse(5 * time.Second)
	_ = conn3.Close()
	if err != nil {
		t.Fatalf("failed to read response for Request 3: %v", err)
	}

	if res3.StatusCode != http.StatusNotModified {
		t.Errorf("Request 3: expected status 304 Not Modified, got %d", res3.StatusCode)
	}
	if len(res3.Body) != 0 {
		t.Errorf("Request 3: expected 0 body bytes for 304 response, got %d bytes", len(res3.Body))
	}
}
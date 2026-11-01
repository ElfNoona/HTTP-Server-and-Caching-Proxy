package proxy

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/http"
	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/proxy/cache"
)

func TestSanitizeHeaders(t *testing.T) {
	orig := map[string]string{
		"host":                "example.com",
		"connection":          "keep-alive, X-Custom-Hop",
		"keep-alive":          "timeout=5",
		"proxy-authorization": "secret",
		"user-agent":          "curl/7.68.0",
		"x-custom-hop":        "temp-value",
		"accept":              "text/html",
	}

	sanitized := SanitizeHeaders(orig)

	// Hop-by-hop headers must be gone
	for _, hop := range []string{"connection", "keep-alive", "proxy-authorization", "x-custom-hop"} {
		if _, ok := sanitized[hop]; ok {
			t.Errorf("expected hop-by-hop header %q to be removed", hop)
		}
	}

	// End-to-end headers must remain
	if sanitized["host"] != "example.com" {
		t.Errorf("expected host header to remain")
	}
	if sanitized["user-agent"] != "curl/7.68.0" {
		t.Errorf("expected user-agent to remain")
	}
	if sanitized["accept"] != "text/html" {
		t.Errorf("expected accept to remain")
	}
}

func TestExtractUpstreamTarget(t *testing.T) {
	defaultUpstream := "origin.local:8080"

	// 1. Absolute URI with port
	req1 := &http.HTTPRequest{
		URI:     "http://example.org:9000/index.html",
		Headers: map[string]string{},
	}
	h1, p1, err := ExtractUpstreamTarget(req1, defaultUpstream)
	if err != nil || h1 != "example.org:9000" || p1 != "/index.html" {
		t.Errorf("case 1 failed: got host=%s, path=%s, err=%v", h1, p1, err)
	}

	// 2. Absolute URI without port (defaults to :80)
	req2 := &http.HTTPRequest{
		URI:     "http://example.org/api/v1",
		Headers: map[string]string{},
	}
	h2, p2, err := ExtractUpstreamTarget(req2, defaultUpstream)
	if err != nil || h2 != "example.org:80" || p2 != "/api/v1" {
		t.Errorf("case 2 failed: got host=%s, path=%s, err=%v", h2, p2, err)
	}

	// 3. Relative URI with Host header
	req3 := &http.HTTPRequest{
		URI:     "/dashboard",
		Headers: map[string]string{"host": "mysite.com:3000"},
	}
	h3, p3, err := ExtractUpstreamTarget(req3, defaultUpstream)
	if err != nil || h3 != "mysite.com:3000" || p3 != "/dashboard" {
		t.Errorf("case 3 failed: got host=%s, path=%s, err=%v", h3, p3, err)
	}

	// 4. Relative URI without Host header (falls back to defaultUpstream)
	req4 := &http.HTTPRequest{
		URI:     "/styles.css",
		Headers: map[string]string{},
	}
	h4, p4, err := ExtractUpstreamTarget(req4, defaultUpstream)
	if err != nil || h4 != defaultUpstream || p4 != "/styles.css" {
		t.Errorf("case 4 failed: got host=%s, path=%s, err=%v", h4, p4, err)
	}
}

func TestFormatUpstreamRequest(t *testing.T) {
	req := &http.HTTPRequest{
		Method: "POST",
		URI:    "/api/upload",
		Body:   []byte("test-payload"),
	}
	sanitized := map[string]string{
		"accept": "application/json",
	}

	raw := FormatUpstreamRequest(req, "localhost:8080", "/api/upload", sanitized)
	rawStr := string(raw)

	if !strings.HasPrefix(rawStr, "POST /api/upload HTTP/1.1\r\n") {
		t.Errorf("expected request line, got: %s", rawStr)
	}
	if !strings.Contains(rawStr, "Host: localhost:8080\r\n") {
		t.Errorf("expected Host header, got: %s", rawStr)
	}
	if !strings.Contains(rawStr, "Connection: keep-alive\r\n") {
		t.Errorf("expected Connection header, got: %s", rawStr)
	}
	if !strings.Contains(rawStr, "Content-Length: 12\r\n") {
		t.Errorf("expected Content-Length header, got: %s", rawStr)
	}
	if !strings.HasSuffix(rawStr, "\r\ntest-payload") {
		t.Errorf("expected body at end, got: %s", rawStr)
	}
}

func TestConnectionPool(t *testing.T) {
	// Spin up a mock TCP listener
	laddr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to resolve TCP addr: %v", err)
	}
	listener, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		t.Fatalf("failed to listen on TCP: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			c, aErr := listener.Accept()
			if aErr != nil {
				return
			}
			// Keep socket open
			go func(conn net.Conn) {
				buf := make([]byte, 100)
				_, _ = conn.Read(buf)
			}(c)
		}
	}()

	pool := NewConnectionPool(2, 500*time.Millisecond, 1*time.Second)
	defer pool.CloseAll()

	serverAddr := listener.Addr().String()

	// Acquire new connection
	conn1, err := pool.Acquire(serverAddr)
	if err != nil {
		t.Fatalf("failed acquiring conn1: %v", err)
	}

	// Release back to pool
	pool.Release(serverAddr, conn1, false)

	// Acquire again: should reuse conn1
	conn2, err := pool.Acquire(serverAddr)
	if err != nil {
		t.Fatalf("failed acquiring conn2: %v", err)
	}
	if conn2 != conn1 {
		t.Errorf("expected reused connection")
	}

	// Release and wait for idle timeout
	pool.Release(serverAddr, conn2, false)
	time.Sleep(600 * time.Millisecond)

	// Next acquire should dial a new connection (expired was pruned)
	conn3, err := pool.Acquire(serverAddr)
	if err != nil {
		t.Fatalf("failed acquiring conn3: %v", err)
	}
	if conn3 == conn2 {
		t.Errorf("expected new connection after timeout expiration")
	}
	pool.Release(serverAddr, conn3, true)
}

func TestProxyServer_EndToEndCache(t *testing.T) {
	// 1. Mock Origin Server
	originAddr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to resolve origin addr: %v", err)
	}
	originListener, err := net.ListenTCP("tcp", originAddr)
	if err != nil {
		t.Fatalf("failed to listen on origin: %v", err)
	}
	defer originListener.Close()

	originHits := 0
	go func() {
		for {
			conn, aErr := originListener.AcceptTCP()
			if aErr != nil {
				return
			}
			go func(c *net.TCPConn) {
				defer c.Close()
				acc := http.NewBufferAccumulator(c)
				for {
					req, rErr := acc.ReadNextRequest(2 * time.Second)
					if rErr != nil {
						return
					}
					originHits++
					res := http.NewResponse(http.StatusOK)
					res.SetHeader("Content-Type", "text/plain")
					res.SetHeader("Cache-Control", "max-age=60")
					res.SetHeader("ETag", "\"origin-etag-123\"")
					res.SetBody([]byte("origin-content"), "text/plain")
					_, _ = res.WriteTo(c)
					if req.Close {
						return
					}
				}
			}(conn)
		}
	}()

	// 2. Start Proxy Server
	proxyAddr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to resolve proxy addr: %v", err)
	}
	proxyListener, err := net.ListenTCP("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("failed to listen on proxy: %v", err)
	}
	defer proxyListener.Close()

	lru := cache.NewLRUCache(10 * 1024 * 1024)
	server := NewProxyServer(proxyListener, originListener.Addr().String(), lru)
	go server.Start(4)
	defer server.Stop()

	proxyTarget := proxyListener.Addr().String()

	originTarget := originListener.Addr().String()

	// 3. Request 1: Cache MISS
	client1, err := net.Dial("tcp", proxyTarget)
	if err != nil {
		t.Fatalf("dial client1 failed: %v", err)
	}
	req1 := fmt.Sprintf("GET /page HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", originTarget)
	_, _ = client1.Write([]byte(req1))
	acc1 := http.NewBufferAccumulator(client1)
	res1, err := acc1.ReadNextResponse(5 * time.Second)
	_ = client1.Close()
	if err != nil {
		t.Fatalf("failed reading response 1: %v", err)
	}
	if res1.GetHeader("X-Cache") != "MISS" {
		t.Errorf("expected X-Cache: MISS on first request, got: %s", res1.GetHeader("X-Cache"))
	}
	if string(res1.Body) != "origin-content" {
		t.Errorf("expected body 'origin-content', got %q", string(res1.Body))
	}
	if originHits != 1 {
		t.Errorf("expected 1 origin hit, got %d", originHits)
	}

	// 4. Request 2: Cache HIT (Origin should not be hit again)
	client2, err := net.Dial("tcp", proxyTarget)
	if err != nil {
		t.Fatalf("dial client2 failed: %v", err)
	}
	req2 := fmt.Sprintf("GET /page HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", originTarget)
	_, _ = client2.Write([]byte(req2))
	acc2 := http.NewBufferAccumulator(client2)
	res2, err := acc2.ReadNextResponse(5 * time.Second)
	_ = client2.Close()
	if err != nil {
		t.Fatalf("failed reading response 2: %v", err)
	}
	if res2.GetHeader("X-Cache") != "HIT" {
		t.Errorf("expected X-Cache: HIT on second request, got: %s", res2.GetHeader("X-Cache"))
	}
	if string(res2.Body) != "origin-content" {
		t.Errorf("expected body 'origin-content', got %q", string(res2.Body))
	}
	if originHits != 1 {
		t.Errorf("origin should NOT be hit on cache hit! originHits = %d", originHits)
	}

	// 5. Request 3: Cache HIT 304 with If-None-Match
	client3, err := net.Dial("tcp", proxyTarget)
	if err != nil {
		t.Fatalf("dial client3 failed: %v", err)
	}
	req3 := fmt.Sprintf("GET /page HTTP/1.1\r\nHost: %s\r\nIf-None-Match: \"origin-etag-123\"\r\nConnection: close\r\n\r\n", originTarget)
	_, _ = client3.Write([]byte(req3))
	acc3 := http.NewBufferAccumulator(client3)
	res3, err := acc3.ReadNextResponse(5 * time.Second)
	_ = client3.Close()
	if err != nil {
		t.Fatalf("failed reading response 3: %v", err)
	}
	if res3.StatusCode != http.StatusNotModified {
		t.Errorf("expected 304 Not Modified, got: %d", res3.StatusCode)
	}
	if res3.GetHeader("X-Cache") != "HIT" {
		t.Errorf("expected X-Cache: HIT on 304 revalidation, got: %s", res3.GetHeader("X-Cache"))
	}
	if originHits != 1 {
		t.Errorf("origin should not be hit on 304 validation! originHits = %d", originHits)
	}
}

package origin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/http"
)

func TestGetMIMEType(t *testing.T) {
	tests := []struct {
		ext      string
		expected string
	}{
		{".html", "text/html; charset=utf-8"},
		{".htm", "text/html; charset=utf-8"},
		{".css", "text/css; charset=utf-8"},
		{".js", "application/javascript; charset=utf-8"},
		{".json", "application/json; charset=utf-8"},
		{".png", "image/png"},
		{".jpg", "image/jpeg"},
		{".jpeg", "image/jpeg"},
		{".gif", "image/gif"},
		{".txt", "text/plain; charset=utf-8"},
		{".bin", "application/octet-stream"},
		{"", "application/octet-stream"},
	}

	for _, tc := range tests {
		if got := GetMIMEType(tc.ext); got != tc.expected {
			t.Errorf("GetMIMEType(%q) = %q, want %q", tc.ext, got, tc.expected)
		}
	}
}

func TestStaticFileServing(t *testing.T) {
	tempDir := t.TempDir()

	indexContent := "<html><body><h1>Hello P1</h1></body></html>"
	cssContent := "body { background: #fff; }"

	if err := os.WriteFile(filepath.Join(tempDir, "index.html"), []byte(indexContent), 0644); err != nil {
		t.Fatalf("failed to write index.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "style.css"), []byte(cssContent), 0644); err != nil {
		t.Fatalf("failed to write style.css: %v", err)
	}

	dispatcher, err := NewStaticDispatcher(tempDir)
	if err != nil {
		t.Fatalf("NewStaticDispatcher failed: %v", err)
	}

	// 1. GET / -> defaults to /index.html
	reqRoot := &http.HTTPRequest{
		Method: "GET",
		URI:    "/",
		Path:   "/",
	}
	resRoot, err := dispatcher.Serve(reqRoot)
	if err != nil {
		t.Fatalf("unexpected error for GET /: %v", err)
	}
	if resRoot.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resRoot.StatusCode)
	}
	if ct := resRoot.GetHeader("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("expected Content-Type text/html; charset=utf-8, got %q", ct)
	}
	if string(resRoot.Body) != indexContent {
		t.Errorf("expected body %q, got %q", indexContent, string(resRoot.Body))
	}

	// 2. GET /style.css
	reqCSS := &http.HTTPRequest{
		Method: "GET",
		URI:    "/style.css",
		Path:   "/style.css",
	}
	resCSS, err := dispatcher.Serve(reqCSS)
	if err != nil {
		t.Fatalf("unexpected error for GET /style.css: %v", err)
	}
	if resCSS.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resCSS.StatusCode)
	}
	if ct := resCSS.GetHeader("Content-Type"); ct != "text/css; charset=utf-8" {
		t.Errorf("expected Content-Type text/css; charset=utf-8, got %q", ct)
	}
	if string(resCSS.Body) != cssContent {
		t.Errorf("expected body %q, got %q", cssContent, string(resCSS.Body))
	}
}

func TestHeadMethod(t *testing.T) {
	tempDir := t.TempDir()
	indexContent := "<html><body>Sample</body></html>"
	if err := os.WriteFile(filepath.Join(tempDir, "index.html"), []byte(indexContent), 0644); err != nil {
		t.Fatalf("failed to write index.html: %v", err)
	}

	dispatcher, err := NewStaticDispatcher(tempDir)
	if err != nil {
		t.Fatalf("NewStaticDispatcher failed: %v", err)
	}

	req := &http.HTTPRequest{
		Method: "HEAD",
		URI:    "/index.html",
		Path:   "/index.html",
	}
	res, err := dispatcher.Serve(req)
	if err != nil {
		t.Fatalf("unexpected error on HEAD: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", res.StatusCode)
	}
	if !res.IsHeadOnly {
		t.Errorf("expected IsHeadOnly=true for HEAD request")
	}
	if len(res.Body) != 0 {
		t.Errorf("expected empty body for HEAD, got %q", string(res.Body))
	}
	expectedCL := "32" // len(indexContent) == 32
	if cl := res.GetHeader("Content-Length"); cl != expectedCL {
		t.Errorf("expected Content-Length %s, got %s", expectedCL, cl)
	}
}

func TestDirectoryTraversalDefense(t *testing.T) {
	tempDir := t.TempDir()
	jailDir := filepath.Join(tempDir, "public")
	if err := os.MkdirAll(jailDir, 0755); err != nil {
		t.Fatalf("failed to create public dir: %v", err)
	}
	// Create a secret file OUTSIDE the jail in tempDir
	secretPath := filepath.Join(tempDir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("TOP_SECRET"), 0644); err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}

	dispatcher, err := NewStaticDispatcher(jailDir)
	if err != nil {
		t.Fatalf("NewStaticDispatcher failed: %v", err)
	}

	traversalURIs := []string{
		"/../../../../etc/passwd",
		"/sub/../../secret.txt",
		"/../secret.txt",
		"/../../secret.txt",
	}

	for _, uri := range traversalURIs {
		req := &http.HTTPRequest{
			Method: "GET",
			URI:    uri,
			Path:   uri,
		}
		res, err := dispatcher.Serve(req)
		if err == nil {
			t.Errorf("expected error for traversal URI %q, got response %+v", uri, res)
			continue
		}
		sErr, ok := err.(*http.StatusError)
		if !ok {
			t.Errorf("expected *http.StatusError, got %T: %v", err, err)
			continue
		}
		if sErr.Code != http.StatusNotFound {
			t.Errorf("expected 404 Not Found for traversal %q, got %d", uri, sErr.Code)
		}
	}
}

func TestUnsupportedMethods(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "index.html"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	dispatcher, err := NewStaticDispatcher(tempDir)
	if err != nil {
		t.Fatalf("NewStaticDispatcher failed: %v", err)
	}

	methods := []string{"POST", "PUT", "DELETE", "PATCH", "OPTIONS"}
	for _, m := range methods {
		req := &http.HTTPRequest{
			Method: m,
			URI:    "/index.html",
			Path:   "/index.html",
		}
		_, err := dispatcher.Serve(req)
		if err == nil {
			t.Errorf("expected error for method %s", m)
			continue
		}
		sErr, ok := err.(*http.StatusError)
		if !ok || sErr.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 Method Not Allowed for %s, got %v", m, err)
		}
	}
}

func TestMissingFile(t *testing.T) {
	tempDir := t.TempDir()
	dispatcher, err := NewStaticDispatcher(tempDir)
	if err != nil {
		t.Fatalf("NewStaticDispatcher failed: %v", err)
	}

	req := &http.HTTPRequest{
		Method: "GET",
		URI:    "/does_not_exist.html",
		Path:   "/does_not_exist.html",
	}
	_, err = dispatcher.Serve(req)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	sErr, ok := err.(*http.StatusError)
	if !ok || sErr.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %v", err)
	}
}

func TestDirectoryIndexResolution(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create sub dir: %v", err)
	}
	subContent := "Sub Index"
	if err := os.WriteFile(filepath.Join(subDir, "index.html"), []byte(subContent), 0644); err != nil {
		t.Fatalf("failed to write sub/index.html: %v", err)
	}

	dispatcher, err := NewStaticDispatcher(tempDir)
	if err != nil {
		t.Fatalf("NewStaticDispatcher failed: %v", err)
	}

	// 1. Directory with trailing slash
	reqSlash := &http.HTTPRequest{
		Method: "GET",
		URI:    "/sub/",
		Path:   "/sub/",
	}
	resSlash, err := dispatcher.Serve(reqSlash)
	if err != nil {
		t.Fatalf("unexpected error for GET /sub/: %v", err)
	}
	if string(resSlash.Body) != subContent {
		t.Errorf("expected %q, got %q", subContent, string(resSlash.Body))
	}

	// 2. Directory without trailing slash (falls back to directory index.html)
	reqNoSlash := &http.HTTPRequest{
		Method: "GET",
		URI:    "/sub",
		Path:   "/sub",
	}
	resNoSlash, err := dispatcher.Serve(reqNoSlash)
	if err != nil {
		t.Fatalf("unexpected error for GET /sub: %v", err)
	}
	if string(resNoSlash.Body) != subContent {
		t.Errorf("expected %q, got %q", subContent, string(resNoSlash.Body))
	}
}
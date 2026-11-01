package origin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/http"
)

// StaticDispatcher handles sandboxed disk file delivery with MIME resolution
// and strict directory traversal defenses (CWE-22).
type StaticDispatcher struct {
	rootDir string // Canonical absolute path to the public root
}

// NewStaticDispatcher creates and initializes a StaticDispatcher for the given directory.
// It verifies that the directory exists and is a valid directory.
func NewStaticDispatcher(dir string) (*StaticDispatcher, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path for %q: %w", dir, err)
	}

	fi, err := os.Stat(absDir)
	if err != nil {
		return nil, fmt.Errorf("static root directory %q does not exist: %w", absDir, err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("path %q is not a directory", absDir)
	}

	return &StaticDispatcher{
		rootDir: filepath.Clean(absDir),
	}, nil
}

// GetMIMEType performs fast lookup of MIME types by file extension.
func GetMIMEType(ext string) string {
	switch strings.ToLower(ext) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".txt":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

// Serve dispatches static disk files for GET and HEAD requests, strictly sandboxed to rootDir.
func (s *StaticDispatcher) Serve(req *http.HTTPRequest) (*http.HTTPResponse, error) {
	// Method Check: only GET and HEAD allowed
	if req.Method != "GET" && req.Method != "HEAD" {
		return nil, http.NewStatusError(http.StatusMethodNotAllowed, "Method Not Allowed")
	}

	// URI Path Normalization & Default File
	reqPath := req.Path
	if qIdx := strings.IndexByte(reqPath, '?'); qIdx != -1 {
		reqPath = reqPath[:qIdx]
	}
	if reqPath == "" || reqPath == "/" {
		reqPath = "/index.html"
	} else if strings.HasSuffix(reqPath, "/") {
		reqPath = reqPath + "index.html"
	}

	// Directory Traversal Guard (Strict Sandboxing)
	// Treat request path as relative to the root directory
	relPath := strings.TrimLeft(reqPath, "/")
	cleanPath := filepath.Clean(filepath.FromSlash(relPath))
	targetPath := filepath.Join(s.rootDir, cleanPath)

	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return nil, http.NewStatusError(http.StatusNotFound, "File Not Found")
	}

	// Boundary Check: verify that absTarget is inside s.rootDir
	jailPrefix := s.rootDir
	if !strings.HasSuffix(jailPrefix, string(filepath.Separator)) {
		jailPrefix += string(filepath.Separator)
	}
	if absTarget != s.rootDir && !strings.HasPrefix(absTarget, jailPrefix) {
		return nil, http.NewStatusError(http.StatusNotFound, "File Not Found")
	}

	// Filesystem Lookup
	fi, err := os.Stat(absTarget)
	if err != nil {
		return nil, http.NewStatusError(http.StatusNotFound, "File Not Found")
	}

	// Directory handling: look for index.html inside the directory
	if fi.IsDir() {
		indexPath := filepath.Join(absTarget, "index.html")
		ifi, ierr := os.Stat(indexPath)
		if ierr != nil || ifi.IsDir() {
			return nil, http.NewStatusError(http.StatusNotFound, "File Not Found")
		}
		absTarget = indexPath
		fi = ifi
	}

	// Response Construction
	res := http.NewResponse(http.StatusOK)
	res.SetHeader("Content-Type", GetMIMEType(filepath.Ext(absTarget)))
	res.SetHeader("Content-Length", fmt.Sprintf("%d", fi.Size()))
	res.SetHeader("Last-Modified", fi.ModTime().UTC().Format(time.RFC1123))

	if req.Method == "HEAD" {
		res.IsHeadOnly = true
		res.Body = nil
		return res, nil
	}

	// Read file contents for GET
	data, readErr := os.ReadFile(absTarget)
	if readErr != nil {
		return nil, http.NewStatusError(http.StatusInternalServerError, "Internal Server Error")
	}
	res.Body = data

	return res, nil
}
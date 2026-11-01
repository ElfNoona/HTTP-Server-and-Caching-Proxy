package proxy

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/http"
)

// hopByHopHeaders defines headers that are hop-by-hop and must be stripped before forwarding (RFC 9110 §7.6.1).
var hopByHopHeaders = map[string]bool{
	"connection":          true,
	"keep-alive":          true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"proxy-connection":    true,
	"te":                  true,
	"trailer":             true,
	"transfer-encoding":   true,
	"upgrade":             true,
}

// SanitizeHeaders strips all hop-by-hop headers from origHeaders, returning a clean map of end-to-end headers.
func SanitizeHeaders(origHeaders map[string]string) map[string]string {
	sanitized := make(map[string]string)

	// Check if Connection header specifies additional hop-by-hop headers to drop
	customHopByHop := make(map[string]bool)
	if connVal, ok := origHeaders["connection"]; ok {
		tokens := strings.Split(connVal, ",")
		for _, t := range tokens {
			customHopByHop[strings.ToLower(strings.TrimSpace(t))] = true
		}
	}

	for k, v := range origHeaders {
		kLower := strings.ToLower(k)
		if hopByHopHeaders[kLower] || customHopByHop[kLower] {
			continue
		}
		sanitized[k] = v
	}

	return sanitized
}

// ExtractUpstreamTarget determines the destination host:port and relative path from an incoming request.
func ExtractUpstreamTarget(req *http.HTTPRequest, defaultUpstream string) (host string, path string, err error) {
	uri := req.URI

	// 1. Absolute URI: http://host[:port]/path
	if strings.HasPrefix(uri, "http://") {
		trimmed := strings.TrimPrefix(uri, "http://")
		slashIdx := strings.IndexByte(trimmed, '/')
		if slashIdx != -1 {
			host = trimmed[:slashIdx]
			path = trimmed[slashIdx:]
		} else {
			host = trimmed
			path = "/"
		}

		if !strings.Contains(host, ":") {
			host = host + ":80"
		}
		return host, path, nil
	}

	// 2. Relative URI: /path
	path = uri
	if path == "" {
		path = "/"
	}

	// Determine upstream host from Host header, falling back to defaultUpstream
	if h, ok := req.Headers["host"]; ok && strings.TrimSpace(h) != "" {
		host = strings.TrimSpace(h)
		if !strings.Contains(host, ":") {
			if strings.Contains(defaultUpstream, ":") {
				defaultHost := defaultUpstream[:strings.Index(defaultUpstream, ":")]
				if host == defaultHost {
					host = defaultUpstream
				} else {
					host = host + ":80"
				}
			} else {
				host = host + ":80"
			}
		}
	} else {
		host = defaultUpstream
	}

	return host, path, nil
}

// FormatUpstreamRequest constructs the raw HTTP/1.1 byte stream to forward to the upstream server.
func FormatUpstreamRequest(req *http.HTTPRequest, targetHost string, targetPath string, sanitizedHeaders map[string]string) []byte {
	var buf bytes.Buffer

	// Request line: METHOD targetPath HTTP/1.1\r\n
	buf.WriteString(fmt.Sprintf("%s %s HTTP/1.1\r\n", req.Method, targetPath))

	// Ensure Host header
	hostVal := targetHost
	buf.WriteString(fmt.Sprintf("Host: %s\r\n", hostVal))

	// Ensure Connection: keep-alive
	buf.WriteString("Connection: keep-alive\r\n")

	// Append sanitized headers (skip Host and Connection as already written)
	for k, v := range sanitizedHeaders {
		kLower := strings.ToLower(k)
		if kLower == "host" || kLower == "connection" || kLower == "content-length" {
			continue
		}
		buf.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}

	// Ensure Content-Length if body is present
	if len(req.Body) > 0 {
		buf.WriteString(fmt.Sprintf("Content-Length: %s\r\n", strconv.Itoa(len(req.Body))))
	}

	// Terminate header block
	buf.WriteString("\r\n")

	// Append body payload if any
	if len(req.Body) > 0 {
		buf.Write(req.Body)
	}

	return buf.Bytes()
}


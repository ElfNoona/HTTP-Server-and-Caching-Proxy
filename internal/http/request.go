package http

import (
	"fmt"
	"strconv"
	"strings"
)

// HTTP protocol framing limits (RFC 9112).
const (
	MaxURILength    = 2048
	MaxHeaderLength = 8192
)

// HTTPRequest represents a parsed HTTP request header and metadata.
type HTTPRequest struct {
	Method        string
	URI           string
	Path          string
	QueryParams   map[string]string
	Proto         string
	Headers       map[string]string // lowercase keys
	ContentLength int64
	IsChunked     bool
	Close         bool
	Body          []byte
}

// ParseRequestHeader parses the raw HTTP request header block into an HTTPRequest struct.
// It enforces RFC 9112 framing rules, token counts, version support, and header specifications.
func ParseRequestHeader(rawHeader []byte) (*HTTPRequest, error) {
	if len(rawHeader) > MaxHeaderLength {
		return nil, NewStatusError(StatusHeaderTooLarge, fmt.Sprintf("header length %d exceeds max allowed %d", len(rawHeader), MaxHeaderLength))
	}

	rawStr := string(rawHeader)
	lines := strings.Split(rawStr, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], "\r")
	}

	var reqLine string
	var headerLines []string
	foundReqLine := false

	for _, l := range lines {
		if !foundReqLine {
			// RFC 9112 Section 2.2: Ignore leading empty line(s) before request line
			if l == "" {
				continue
			}
			reqLine = l
			foundReqLine = true
		} else {
			if l == "" {
				// Empty line signals end of header section
				break
			}
			headerLines = append(headerLines, l)
		}
	}

	if !foundReqLine {
		return nil, NewStatusError(StatusBadRequest, "empty request line")
	}

	// Parse request-line: METHOD URI PROTO
	tokens := strings.Split(reqLine, " ")
	if len(tokens) != 3 {
		return nil, NewStatusError(StatusBadRequest, "request line must contain exactly 3 tokens")
	}

	method := tokens[0]
	uri := tokens[1]
	proto := tokens[2]

	if method == "" || uri == "" || proto == "" {
		return nil, NewStatusError(StatusBadRequest, "empty token in request line")
	}

	if len(uri) > MaxURILength {
		return nil, NewStatusError(StatusURITooLong, fmt.Sprintf("URI length %d exceeds max allowed %d", len(uri), MaxURILength))
	}

	if proto != "HTTP/1.1" && proto != "HTTP/1.0" {
		return nil, NewStatusError(StatusHTTPVersionNotSupported, fmt.Sprintf("unsupported HTTP version: %s", proto))
	}

	// Extract Path and QueryParams
	var path string
	queryParams := make(map[string]string)
	if qIdx := strings.IndexByte(uri, '?'); qIdx != -1 {
		path = uri[:qIdx]
		rawQuery := uri[qIdx+1:]
		if rawQuery != "" {
			parts := strings.Split(rawQuery, "&")
			for _, part := range parts {
				if part == "" {
					continue
				}
				k, v, found := strings.Cut(part, "=")
				if found {
					queryParams[k] = v
				} else {
					queryParams[k] = ""
				}
			}
		}
	} else {
		path = uri
	}

	// Parse headers line by line
	headers := make(map[string]string)
	for _, line := range headerLines {
		colonIdx := strings.IndexByte(line, ':')
		if colonIdx == -1 {
			return nil, NewStatusError(StatusBadRequest, "malformed header: missing colon")
		}

		rawKey := line[:colonIdx]
		// RFC 9112 Section 5.1: No whitespace allowed between header field name and colon
		if strings.ContainsAny(rawKey, " \t") {
			return nil, NewStatusError(StatusBadRequest, "malformed header: whitespace before colon")
		}

		key := strings.ToLower(strings.TrimSpace(rawKey))
		if key == "" {
			return nil, NewStatusError(StatusBadRequest, "malformed header: empty field name")
		}

		val := strings.TrimSpace(line[colonIdx+1:])
		if existing, exists := headers[key]; exists {
			headers[key] = existing + ", " + val
		} else {
			headers[key] = val
		}
	}

	// Detect "transfer-encoding: chunked"
	var isChunked bool
	if te, ok := headers["transfer-encoding"]; ok {
		teParts := strings.Split(strings.ToLower(te), ",")
		for _, part := range teParts {
			if strings.TrimSpace(part) == "chunked" {
				isChunked = true
				break
			}
		}
	}

	// Detect "content-length"
	var contentLength int64
	if clStr, ok := headers["content-length"]; ok {
		cl, err := strconv.ParseInt(strings.TrimSpace(clStr), 10, 64)
		if err != nil || cl < 0 {
			return nil, NewStatusError(StatusBadRequest, "malformed or negative Content-Length")
		}
		contentLength = cl
	}

	// Determine Close bool
	// HTTP/1.0 default true unless Connection: keep-alive
	// HTTP/1.1 default false unless Connection: close
	connVal := strings.ToLower(headers["connection"])
	var shouldClose bool
	if proto == "HTTP/1.0" {
		shouldClose = true
		if strings.Contains(connVal, "keep-alive") {
			shouldClose = false
		}
	} else { // HTTP/1.1
		shouldClose = false
		if strings.Contains(connVal, "close") {
			shouldClose = true
		}
	}

	return &HTTPRequest{
		Method:        method,
		URI:           uri,
		Path:          path,
		QueryParams:   queryParams,
		Proto:         proto,
		Headers:       headers,
		ContentLength: contentLength,
		IsChunked:     isChunked,
		Close:         shouldClose,
		Body:          nil,
	}, nil
}


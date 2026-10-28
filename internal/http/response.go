package http

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// HTTPResponse represents an outgoing HTTP/1.1 response message.
type HTTPResponse struct {
	Proto      string
	StatusCode int
	Headers    map[string]string
	Body       []byte
	IsHeadOnly bool
}

// canonicalHeaderKey normalizes header field names into standard HTTP-casing (e.g., Content-Type).
func canonicalHeaderKey(s string) string {
	b := []byte(s)
	upper := true
	for i, c := range b {
		if upper && 'a' <= c && c <= 'z' {
			b[i] -= 'a' - 'A'
		} else if !upper && 'A' <= c && c <= 'Z' {
			b[i] += 'a' - 'A'
		}
		upper = c == '-'
	}
	return string(b)
}

// NewResponse initializes an HTTPResponse with standard default headers.
func NewResponse(code int) *HTTPResponse {
	res := &HTTPResponse{
		Proto:      "HTTP/1.1",
		StatusCode: code,
		Headers:    make(map[string]string),
	}
	res.SetHeader("Server", "P1-Raw-Socket-Server")
	res.SetHeader("Date", time.Now().UTC().Format(time.RFC1123))
	return res
}

// NewErrorResponse creates an HTTPResponse suited for reporting errors back to the client.
func NewErrorResponse(err error) *HTTPResponse {
	code := StatusInternalServerError
	msg := ""

	if sErr, ok := err.(*StatusError); ok && sErr != nil {
		code = sErr.Code
		msg = sErr.Message
	} else if err != nil {
		msg = err.Error()
	}

	statusTxt := StatusText(code)
	if statusTxt == "" {
		statusTxt = "Error"
	}

	res := NewResponse(code)
	res.SetHeader("Connection", "close")

	var bodyStr string
	if msg != "" && msg != statusTxt {
		bodyStr = fmt.Sprintf("%d %s: %s\n", code, statusTxt, msg)
	} else {
		bodyStr = fmt.Sprintf("%d %s\n", code, statusTxt)
	}

	res.SetBody([]byte(bodyStr), "text/plain; charset=utf-8")
	return res
}

// SetHeader sets a response header, handling keys case-insensitively.
func (res *HTTPResponse) SetHeader(key, value string) {
	if res.Headers == nil {
		res.Headers = make(map[string]string)
	}
	cKey := canonicalHeaderKey(key)
	for k := range res.Headers {
		if strings.EqualFold(k, key) && k != cKey {
			delete(res.Headers, k)
		}
	}
	res.Headers[cKey] = value
}

// GetHeader retrieves a response header case-insensitively.
func (res *HTTPResponse) GetHeader(key string) string {
	if res.Headers == nil {
		return ""
	}
	cKey := canonicalHeaderKey(key)
	if v, ok := res.Headers[cKey]; ok {
		return v
	}
	for k, v := range res.Headers {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

// SetBody sets the response body, Content-Type, and automatically updates Content-Length.
func (res *HTTPResponse) SetBody(body []byte, contentType string) {
	res.Body = body
	if contentType != "" {
		res.SetHeader("Content-Type", contentType)
	}
	res.SetHeader("Content-Length", strconv.Itoa(len(body)))
}

// SerializeHeaders serializes the status-line and headers into wire-format HTTP bytes.
func (res *HTTPResponse) SerializeHeaders() []byte {
	proto := res.Proto
	if proto == "" {
		proto = "HTTP/1.1"
	}

	statusTxt := StatusText(res.StatusCode)
	if statusTxt == "" {
		statusTxt = "Unknown"
	}

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("%s %d %s\r\n", proto, res.StatusCode, statusTxt))

	for k, v := range res.Headers {
		buf.WriteString(k)
		buf.WriteString(": ")
		buf.WriteString(v)
		buf.WriteString("\r\n")
	}

	buf.WriteString("\r\n")
	return buf.Bytes()
}

// Serialize converts the entire response (headers and optional body) into wire-format bytes.
func (res *HTTPResponse) Serialize() []byte {
	headers := res.SerializeHeaders()
	if res.IsHeadOnly || len(res.Body) == 0 {
		return headers
	}

	result := make([]byte, len(headers)+len(res.Body))
	copy(result, headers)
	copy(result[len(headers):], res.Body)
	return result
}

// WriteTo writes the serialized response to an io.Writer (e.g., net.Conn).
func (res *HTTPResponse) WriteTo(w io.Writer) (int64, error) {
	data := res.Serialize()
	n, err := w.Write(data)
	return int64(n), err
}

// ParseResponseHeader parses raw HTTP response headers into an HTTPResponse struct.
func ParseResponseHeader(rawHeader []byte) (*HTTPResponse, error) {
	if len(rawHeader) > MaxHeaderLength {
		return nil, NewStatusError(StatusHeaderTooLarge, "response header fields too large")
	}

	rawStr := string(rawHeader)
	lines := strings.Split(rawStr, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], "\r")
	}

	var statusLine string
	var headerLines []string
	foundStatusLine := false

	for _, l := range lines {
		if !foundStatusLine {
			if l == "" {
				continue
			}
			statusLine = l
			foundStatusLine = true
		} else {
			if l == "" {
				break
			}
			headerLines = append(headerLines, l)
		}
	}

	if !foundStatusLine {
		return nil, NewStatusError(StatusBadRequest, "empty response line")
	}

	// Status line format: PROTO STATUS REASON
	parts := strings.SplitN(statusLine, " ", 3)
	if len(parts) < 2 {
		return nil, NewStatusError(StatusBadRequest, "malformed response status line")
	}

	proto := parts[0]
	code, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, NewStatusError(StatusBadRequest, "invalid response status code")
	}

	headers := make(map[string]string)
	for _, line := range headerLines {
		colonIdx := strings.IndexByte(line, ':')
		if colonIdx == -1 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colonIdx]))
		val := strings.TrimSpace(line[colonIdx+1:])
		headers[key] = val
	}

	return &HTTPResponse{
		Proto:      proto,
		StatusCode: code,
		Headers:    headers,
	}, nil
}

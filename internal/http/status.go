package http

import "fmt"

// HTTP status codes defined in RFC 9112 and RFC 9110.
const (
	StatusOK                      = 200
	StatusNotModified             = 304
	StatusBadRequest              = 400
	StatusNotFound                = 404
	StatusMethodNotAllowed        = 405
	StatusURITooLong              = 414
	StatusHeaderTooLarge          = 431
	StatusInternalServerError     = 500
	StatusHTTPVersionNotSupported = 505

	// Aliases for standard descriptive naming
	StatusRequestURITooLong           = StatusURITooLong
	StatusRequestHeaderFieldsTooLarge = StatusHeaderTooLarge
)

// StatusText returns the standard RFC reason phrase for a given HTTP status code.
// Returns an empty string if the code is unknown.
func StatusText(code int) string {
	switch code {
	case StatusOK:
		return "OK"
	case StatusNotModified:
		return "Not Modified"
	case StatusBadRequest:
		return "Bad Request"
	case StatusNotFound:
		return "Not Found"
	case StatusMethodNotAllowed:
		return "Method Not Allowed"
	case StatusURITooLong:
		return "URI Too Long"
	case StatusHeaderTooLarge:
		return "Request Header Fields Too Large"
	case StatusInternalServerError:
		return "Internal Server Error"
	case StatusHTTPVersionNotSupported:
		return "HTTP Version Not Supported"
	default:
		return ""
	}
}

// StatusError represents an HTTP protocol error with an associated status code.
type StatusError struct {
	Code    int
	Message string
}

// Error formats the StatusError as a human-readable string.
func (e *StatusError) Error() string {
	text := StatusText(e.Code)
	if text == "" {
		text = "Unknown Status"
	}
	if e.Message != "" {
		return fmt.Sprintf("HTTP %d %s: %s", e.Code, text, e.Message)
	}
	return fmt.Sprintf("HTTP %d %s", e.Code, text)
}

// NewStatusError constructs a new StatusError with a specific code and explanatory message.
func NewStatusError(code int, msg string) *StatusError {
	return &StatusError{
		Code:    code,
		Message: msg,
	}
}

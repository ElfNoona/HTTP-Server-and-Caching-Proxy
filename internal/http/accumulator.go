package http

import (
	"bytes"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// BufferAccumulator manages a sliding-window byte buffer over a net.Conn.
// It handles TCP fragmentation, coalesced pipelined frames, and chunked encoding.
type BufferAccumulator struct {
	conn net.Conn
	buf  []byte
}

// NewBufferAccumulator creates a new BufferAccumulator for the given connection
// with an initial slice capacity of at least 4096 bytes.
func NewBufferAccumulator(conn net.Conn) *BufferAccumulator {
	return &BufferAccumulator{
		conn: conn,
		buf:  make([]byte, 0, 4096),
	}
}

// Reset clears the internal buffer while keeping allocated capacity.
func (a *BufferAccumulator) Reset() {
	a.buf = a.buf[:0]
}

// Buffered returns the number of unconsumed bytes currently resting in the buffer.
func (a *BufferAccumulator) Buffered() int {
	return len(a.buf)
}

// ReadNextRequest parses and returns the next complete HTTPRequest from the stream.
// It sets a socket read deadline whenever performing network I/O.
func (a *BufferAccumulator) ReadNextRequest(timeout time.Duration) (*HTTPRequest, error) {
	// Continuous Ingestion Loop for headers
	for {
		delimIdx := bytes.Index(a.buf, []byte("\r\n\r\n"))
		if delimIdx == -1 {
			// Delimiter NOT found: guard check buffer limit
			if len(a.buf) > MaxHeaderLength {
				return nil, NewStatusError(StatusHeaderTooLarge, "Request Header Fields Too Large")
			}

			if timeout > 0 {
				_ = a.conn.SetReadDeadline(time.Now().Add(timeout))
			} else {
				_ = a.conn.SetReadDeadline(time.Time{})
			}

			tmp := make([]byte, 2048)
			n, err := a.conn.Read(tmp)
			if n > 0 {
				a.buf = append(a.buf, tmp[:n]...)
			}
			if err != nil {
				if err == io.EOF && len(a.buf) == 0 {
					return nil, io.EOF
				}
				return nil, err
			}
			continue
		}

		// Delimiter IS found at delimIdx
		if delimIdx > MaxHeaderLength {
			return nil, NewStatusError(StatusHeaderTooLarge, "Request Header Fields Too Large")
		}

		rawHeader := a.buf[:delimIdx]
		// Advance buffer past headers (preserving residual body or pipelined bytes)
		a.buf = a.buf[delimIdx+4:]

		req, err := ParseRequestHeader(rawHeader)
		if err != nil {
			return nil, err
		}

		// Body extraction
		if req.ContentLength > 0 {
			body, readErr := a.readExact(req.ContentLength, timeout)
			if readErr != nil {
				return nil, readErr
			}
			req.Body = body
		} else if req.IsChunked {
			for {
				decoded, bytesConsumed, complete, chunkErr := DecodeChunked(a.buf)
				if chunkErr != nil {
					return nil, chunkErr
				}
				if complete {
					a.buf = a.buf[bytesConsumed:]
					req.Body = decoded
					break
				}

				// Buffer more bytes from socket
				if timeout > 0 {
					_ = a.conn.SetReadDeadline(time.Now().Add(timeout))
				} else {
					_ = a.conn.SetReadDeadline(time.Time{})
				}

				tmp := make([]byte, 2048)
				n, readErr := a.conn.Read(tmp)
				if n > 0 {
					a.buf = append(a.buf, tmp[:n]...)
				}
				if readErr != nil {
					if readErr == io.EOF {
						return nil, io.ErrUnexpectedEOF
					}
					return nil, readErr
				}
			}
		} else {
			// Case C: No body expected
			req.Body = nil
		}

		return req, nil
	}
}

// readExact drains up to length bytes from a.buf, reading any remainder from a.conn.
func (a *BufferAccumulator) readExact(length int64, timeout time.Duration) ([]byte, error) {
	if length <= 0 {
		return nil, nil
	}
	needed := int(length)
	result := make([]byte, needed)

	// 1. Drain available bytes from a.buf
	buffered := len(a.buf)
	if buffered >= needed {
		copy(result, a.buf[:needed])
		a.buf = a.buf[needed:]
		return result, nil
	}

	copy(result, a.buf)
	readSoFar := buffered
	a.buf = a.buf[:0]

	// 2. Loop read remaining bytes from a.conn
	for readSoFar < needed {
		if timeout > 0 {
			_ = a.conn.SetReadDeadline(time.Now().Add(timeout))
		} else {
			_ = a.conn.SetReadDeadline(time.Time{})
		}

		n, err := a.conn.Read(result[readSoFar:])
		if n > 0 {
			readSoFar += n
		}
		if err != nil {
			if readSoFar == needed {
				return result, nil
			}
			if err == io.EOF {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
	}

	return result, nil
}

// ReadNextResponse parses and returns the next complete HTTPResponse from the stream.
// It sets a socket read deadline whenever performing network I/O.
func (a *BufferAccumulator) ReadNextResponse(timeout time.Duration) (*HTTPResponse, error) {
	for {
		delimIdx := bytes.Index(a.buf, []byte("\r\n\r\n"))
		if delimIdx == -1 {
			if len(a.buf) > MaxHeaderLength {
				return nil, NewStatusError(StatusHeaderTooLarge, "Response Header Fields Too Large")
			}

			if timeout > 0 {
				_ = a.conn.SetReadDeadline(time.Now().Add(timeout))
			} else {
				_ = a.conn.SetReadDeadline(time.Time{})
			}

			tmp := make([]byte, 2048)
			n, err := a.conn.Read(tmp)
			if n > 0 {
				a.buf = append(a.buf, tmp[:n]...)
			}
			if err != nil {
				if err == io.EOF && len(a.buf) == 0 {
					return nil, io.EOF
				}
				return nil, err
			}
			continue
		}

		if delimIdx > MaxHeaderLength {
			return nil, NewStatusError(StatusHeaderTooLarge, "Response Header Fields Too Large")
		}

		rawHeader := a.buf[:delimIdx]
		a.buf = a.buf[delimIdx+4:]

		res, err := ParseResponseHeader(rawHeader)
		if err != nil {
			return nil, err
		}

		// Body extraction
		// 304 Not Modified and 204 No Content must not contain a body
		if res.StatusCode == StatusNotModified || res.StatusCode == 204 {
			res.Body = nil
			return res, nil
		}

		isChunked := false
		if te, ok := res.Headers["transfer-encoding"]; ok {
			if strings.Contains(strings.ToLower(te), "chunked") {
				isChunked = true
			}
		}

		var contentLength int64
		if clStr, ok := res.Headers["content-length"]; ok {
			if cl, clErr := strconv.ParseInt(strings.TrimSpace(clStr), 10, 64); clErr == nil && cl >= 0 {
				contentLength = cl
			}
		}

		if isChunked {
			for {
				decoded, bytesConsumed, complete, chunkErr := DecodeChunked(a.buf)
				if chunkErr != nil {
					return nil, chunkErr
				}
				if complete {
					a.buf = a.buf[bytesConsumed:]
					res.Body = decoded
					break
				}

				if timeout > 0 {
					_ = a.conn.SetReadDeadline(time.Now().Add(timeout))
				} else {
					_ = a.conn.SetReadDeadline(time.Time{})
				}

				tmp := make([]byte, 2048)
				n, readErr := a.conn.Read(tmp)
				if n > 0 {
					a.buf = append(a.buf, tmp[:n]...)
				}
				if readErr != nil {
					if readErr == io.EOF {
						return nil, io.ErrUnexpectedEOF
					}
					return nil, readErr
				}
			}
		} else if contentLength > 0 {
			body, readErr := a.readExact(contentLength, timeout)
			if readErr != nil {
				return nil, readErr
			}
			res.Body = body
		}

		return res, nil
	}
}

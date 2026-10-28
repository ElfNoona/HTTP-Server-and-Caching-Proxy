package http

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// EncodeChunk formats data into an HTTP/1.1 chunk: <hex-length>\r\n<data>\r\n.
// In accordance with RFC 9112 Section 7.1, if data is empty, it returns nil.
func EncodeChunk(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	hexLen := strconv.FormatInt(int64(len(data)), 16)
	buf := make([]byte, 0, len(hexLen)+2+len(data)+2)
	buf = append(buf, hexLen...)
	buf = append(buf, "\r\n"...)
	buf = append(buf, data...)
	buf = append(buf, "\r\n"...)
	return buf
}

// EncodeFinalChunk returns the terminating HTTP/1.1 chunk: 0\r\n\r\n.
func EncodeFinalChunk() []byte {
	return []byte("0\r\n\r\n")
}

// WriteChunk writes an encoded chunk of data to the writer w.
func WriteChunk(w io.Writer, data []byte) error {
	chunk := EncodeChunk(data)
	if len(chunk) == 0 {
		return nil
	}
	_, err := w.Write(chunk)
	return err
}

// WriteFinalChunk writes the terminating 0\r\n\r\n chunk to the writer w.
func WriteFinalChunk(w io.Writer) error {
	_, err := w.Write(EncodeFinalChunk())
	return err
}

// ChunkedDecoder is a state machine helper for decoding chunked transfer encoding streams.
type ChunkedDecoder struct {
	buf []byte
}

// NewChunkedDecoder creates a new ChunkedDecoder instance with an internal buffer.
func NewChunkedDecoder() *ChunkedDecoder {
	return &ChunkedDecoder{
		buf: make([]byte, 0, 4096),
	}
}

// Reset clears the internal buffer of the decoder.
func (d *ChunkedDecoder) Reset() {
	d.buf = d.buf[:0]
}

// DecodeChunked is a convenience method that invokes the package-level DecodeChunked function.
func (d *ChunkedDecoder) DecodeChunked(raw []byte) (decoded []byte, bytesConsumed int, complete bool, err error) {
	return DecodeChunked(raw)
}

// Feed appends incoming stream bytes and parses all available completed chunks.
// It slides the internal buffer past consumed bytes and returns the newly decoded payload.
func (d *ChunkedDecoder) Feed(incoming []byte) (decoded []byte, complete bool, err error) {
	d.buf = append(d.buf, incoming...)
	dec, consumed, comp, parseErr := DecodeChunked(d.buf)
	if parseErr != nil {
		return nil, false, parseErr
	}
	if consumed > 0 {
		copy(d.buf, d.buf[consumed:])
		d.buf = d.buf[:len(d.buf)-consumed]
	}
	return dec, comp, nil
}

// DecodeChunked parses a raw byte slice containing chunked transfer data according to RFC 9112 Section 7.1.
//
// Return values:
// - decoded: The extracted chunk payloads concatenated together.
// - bytesConsumed: Total bytes consumed from raw (including hex headers, CRLFs, and terminating chunk).
// - complete: True when the terminating 0-size chunk and its CRLF / trailers have been fully parsed.
// - err: Non-nil if protocol formatting or hex lengths are invalid (returns StatusBadRequest).
//
// If the raw stream contains an incomplete chunk or boundary, it returns complete = false and
// bytesConsumed = 0 without error, allowing the caller to buffer more bytes before retrying.
func DecodeChunked(raw []byte) (decoded []byte, bytesConsumed int, complete bool, err error) {
	offset := 0
	var out []byte

	for {
		// 1. Find \r\n marking the end of the hex size line
		crlfIdx := bytes.Index(raw[offset:], []byte("\r\n"))
		if crlfIdx == -1 {
			// Incomplete: need more bytes for the hex line
			return nil, 0, false, nil
		}

		hexLine := raw[offset : offset+crlfIdx]

		// 2. Parse hex length (stripping any optional chunk extensions separated by ;)
		if semi := bytes.IndexByte(hexLine, ';'); semi != -1 {
			hexLine = hexLine[:semi]
		}
		hexStr := strings.TrimSpace(string(hexLine))
		if hexStr == "" {
			return nil, 0, false, NewStatusError(StatusBadRequest, "empty chunk size line")
		}

		chunkLen, parseErr := strconv.ParseInt(hexStr, 16, 64)
		if parseErr != nil || chunkLen < 0 {
			return nil, 0, false, NewStatusError(StatusBadRequest, fmt.Sprintf("invalid chunk size hex: %s", hexStr))
		}

		chunkDataStart := offset + crlfIdx + 2

		// 3. If length == 0: terminating chunk
		if chunkLen == 0 {
			if len(raw) < chunkDataStart {
				return nil, 0, false, nil
			}

			// Check for immediate terminating CRLF (no trailers)
			if bytes.HasPrefix(raw[chunkDataStart:], []byte("\r\n")) {
				totalConsumed := chunkDataStart + 2
				return out, totalConsumed, true, nil
			}

			// Check for trailers followed by terminating CRLF (\r\n\r\n)
			trailerEnd := bytes.Index(raw[chunkDataStart:], []byte("\r\n\r\n"))
			if trailerEnd != -1 {
				totalConsumed := chunkDataStart + trailerEnd + 4
				return out, totalConsumed, true, nil
			}

			// Incomplete trailer bytes received
			return nil, 0, false, nil
		}

		// 4. If length > 0: verify that raw slice contains at least (chunkDataStart + chunkLen + 2) bytes
		chunkDataEnd := chunkDataStart + int(chunkLen)
		if len(raw) < chunkDataEnd+2 {
			// Incomplete chunk payload or trailing CRLF
			return nil, 0, false, nil
		}

		// Verify trailing CRLF after chunk data
		if !bytes.Equal(raw[chunkDataEnd:chunkDataEnd+2], []byte("\r\n")) {
			return nil, 0, false, NewStatusError(StatusBadRequest, "missing CRLF after chunk data")
		}

		// 6. Slice and append payload bytes
		out = append(out, raw[chunkDataStart:chunkDataEnd]...)

		// 7. Advance offset past chunk payload and trailing \r\n
		offset = chunkDataEnd + 2
	}
}


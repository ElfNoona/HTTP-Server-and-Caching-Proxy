package http

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestStatusConstantsAndText(t *testing.T) {
	tests := []struct {
		code     int
		expected string
	}{
		{StatusOK, "OK"},
		{StatusNotModified, "Not Modified"},
		{StatusBadRequest, "Bad Request"},
		{StatusNotFound, "Not Found"},
		{StatusMethodNotAllowed, "Method Not Allowed"},
		{StatusURITooLong, "URI Too Long"},
		{StatusHeaderTooLarge, "Request Header Fields Too Large"},
		{StatusInternalServerError, "Internal Server Error"},
		{StatusHTTPVersionNotSupported, "HTTP Version Not Supported"},
		{999, ""},
	}

	for _, tc := range tests {
		if got := StatusText(tc.code); got != tc.expected {
			t.Errorf("StatusText(%d) = %q, want %q", tc.code, got, tc.expected)
		}
	}
}

func TestStatusError(t *testing.T) {
	err := NewStatusError(StatusBadRequest, "missing header")
	if err.Code != 400 {
		t.Errorf("expected code 400, got %d", err.Code)
	}
	if err.Message != "missing header" {
		t.Errorf("expected message 'missing header', got %q", err.Message)
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "missing header") {
		t.Errorf("unexpected Error() string: %s", err.Error())
	}
}

func TestParseRequestHeader_ValidHTTP11(t *testing.T) {
	raw := []byte("GET /index.html HTTP/1.1\r\nHost: example.com\r\nUser-Agent: curl/7.68.0\r\n\r\n")
	req, err := ParseRequestHeader(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Method != "GET" {
		t.Errorf("expected Method GET, got %s", req.Method)
	}
	if req.URI != "/index.html" {
		t.Errorf("expected URI /index.html, got %s", req.URI)
	}
	if req.Path != "/index.html" {
		t.Errorf("expected Path /index.html, got %s", req.Path)
	}
	if req.Proto != "HTTP/1.1" {
		t.Errorf("expected Proto HTTP/1.1, got %s", req.Proto)
	}
	if req.Headers["host"] != "example.com" {
		t.Errorf("expected host header example.com, got %s", req.Headers["host"])
	}
	if req.Headers["user-agent"] != "curl/7.68.0" {
		t.Errorf("expected user-agent header curl/7.68.0, got %s", req.Headers["user-agent"])
	}
	if req.Close {
		t.Errorf("expected Close=false for HTTP/1.1 default, got %v", req.Close)
	}
	if req.IsChunked {
		t.Errorf("expected IsChunked=false, got %v", req.IsChunked)
	}
}

func TestParseRequestHeader_QueryParams(t *testing.T) {
	raw := []byte("GET /api/search?q=golang&limit=10&flag HTTP/1.1\r\nHost: localhost\r\n\r\n")
	req, err := ParseRequestHeader(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Path != "/api/search" {
		t.Errorf("expected Path /api/search, got %s", req.Path)
	}
	if req.QueryParams["q"] != "golang" {
		t.Errorf("expected q=golang, got %s", req.QueryParams["q"])
	}
	if req.QueryParams["limit"] != "10" {
		t.Errorf("expected limit=10, got %s", req.QueryParams["limit"])
	}
	if val, ok := req.QueryParams["flag"]; !ok || val != "" {
		t.Errorf("expected flag='', got %s (exists=%v)", val, ok)
	}
}

func TestParseRequestHeader_ConnectionClose(t *testing.T) {
	// HTTP/1.1 with Connection: close
	raw11 := []byte("GET / HTTP/1.1\r\nConnection: close\r\n\r\n")
	req11, err := ParseRequestHeader(raw11)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !req11.Close {
		t.Errorf("expected Close=true for HTTP/1.1 with Connection: close")
	}

	// HTTP/1.0 default close
	raw10 := []byte("GET / HTTP/1.0\r\n\r\n")
	req10, err := ParseRequestHeader(raw10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !req10.Close {
		t.Errorf("expected Close=true for HTTP/1.0 default")
	}

	// HTTP/1.0 with Connection: keep-alive
	raw10Keep := []byte("GET / HTTP/1.0\r\nConnection: keep-alive\r\n\r\n")
	req10Keep, err := ParseRequestHeader(raw10Keep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req10Keep.Close {
		t.Errorf("expected Close=false for HTTP/1.0 with Connection: keep-alive")
	}
}

func TestParseRequestHeader_ContentLengthAndChunked(t *testing.T) {
	// Content-Length
	rawCL := []byte("POST /upload HTTP/1.1\r\nContent-Length: 1024\r\n\r\n")
	reqCL, err := ParseRequestHeader(rawCL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqCL.ContentLength != 1024 {
		t.Errorf("expected ContentLength=1024, got %d", reqCL.ContentLength)
	}

	// Invalid Content-Length
	rawBadCL := []byte("POST /upload HTTP/1.1\r\nContent-Length: -5\r\n\r\n")
	_, err = ParseRequestHeader(rawBadCL)
	if err == nil {
		t.Fatal("expected error for negative Content-Length")
	}
	if sErr, ok := err.(*StatusError); !ok || sErr.Code != StatusBadRequest {
		t.Errorf("expected StatusBadRequest (400), got %v", err)
	}

	rawMalformedCL := []byte("POST /upload HTTP/1.1\r\nContent-Length: abc\r\n\r\n")
	_, err = ParseRequestHeader(rawMalformedCL)
	if err == nil {
		t.Fatal("expected error for malformed Content-Length")
	}
	if sErr, ok := err.(*StatusError); !ok || sErr.Code != StatusBadRequest {
		t.Errorf("expected StatusBadRequest (400), got %v", err)
	}

	// Chunked Transfer-Encoding
	rawChunked := []byte("POST /stream HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n")
	reqChunked, err := ParseRequestHeader(rawChunked)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reqChunked.IsChunked {
		t.Errorf("expected IsChunked=true")
	}
}

func TestParseRequestHeader_LimitsAndErrors(t *testing.T) {
	// MaxHeaderLength
	hugeHeader := make([]byte, MaxHeaderLength+10)
	copy(hugeHeader, []byte("GET / HTTP/1.1\r\nFoo: "))
	for i := 20; i < len(hugeHeader)-4; i++ {
		hugeHeader[i] = 'A'
	}
	copy(hugeHeader[len(hugeHeader)-4:], []byte("\r\n\r\n"))
	_, err := ParseRequestHeader(hugeHeader)
	if err == nil {
		t.Fatal("expected error for exceeding MaxHeaderLength")
	}
	if sErr, ok := err.(*StatusError); !ok || sErr.Code != StatusHeaderTooLarge {
		t.Errorf("expected StatusHeaderTooLarge (431), got %v", err)
	}

	// MaxURILength
	longURI := "/" + strings.Repeat("a", MaxURILength+1)
	rawLongURI := []byte("GET " + longURI + " HTTP/1.1\r\n\r\n")
	_, err = ParseRequestHeader(rawLongURI)
	if err == nil {
		t.Fatal("expected error for exceeding MaxURILength")
	}
	if sErr, ok := err.(*StatusError); !ok || sErr.Code != StatusURITooLong {
		t.Errorf("expected StatusURITooLong (414), got %v", err)
	}

	// Malformed request line: 2 tokens
	_, err = ParseRequestHeader([]byte("GET /\r\n\r\n"))
	if err == nil {
		t.Fatal("expected error for 2 tokens")
	}
	if sErr, ok := err.(*StatusError); !ok || sErr.Code != StatusBadRequest {
		t.Errorf("expected StatusBadRequest (400), got %v", err)
	}

	// Malformed request line: 4 tokens
	_, err = ParseRequestHeader([]byte("GET / extra HTTP/1.1\r\n\r\n"))
	if err == nil {
		t.Fatal("expected error for 4 tokens")
	}
	if sErr, ok := err.(*StatusError); !ok || sErr.Code != StatusBadRequest {
		t.Errorf("expected StatusBadRequest (400), got %v", err)
	}

	// Unsupported protocol: HTTP/2.0
	_, err = ParseRequestHeader([]byte("GET / HTTP/2.0\r\n\r\n"))
	if err == nil {
		t.Fatal("expected error for HTTP/2.0")
	}
	if sErr, ok := err.(*StatusError); !ok || sErr.Code != StatusHTTPVersionNotSupported {
		t.Errorf("expected StatusHTTPVersionNotSupported (505), got %v", err)
	}

	// Malformed header missing colon
	_, err = ParseRequestHeader([]byte("GET / HTTP/1.1\r\nInvalidHeaderLine\r\n\r\n"))
	if err == nil {
		t.Fatal("expected error for missing colon")
	}
	if sErr, ok := err.(*StatusError); !ok || sErr.Code != StatusBadRequest {
		t.Errorf("expected StatusBadRequest (400), got %v", err)
	}

	// Malformed header whitespace before colon
	_, err = ParseRequestHeader([]byte("GET / HTTP/1.1\r\nHost : localhost\r\n\r\n"))
	if err == nil {
		t.Fatal("expected error for whitespace before colon")
	}
	if sErr, ok := err.(*StatusError); !ok || sErr.Code != StatusBadRequest {
		t.Errorf("expected StatusBadRequest (400), got %v", err)
	}
}

func TestNewResponse(t *testing.T) {
	res := NewResponse(StatusOK)
	if res.StatusCode != 200 {
		t.Errorf("expected 200, got %d", res.StatusCode)
	}
	if res.Proto != "HTTP/1.1" {
		t.Errorf("expected HTTP/1.1, got %s", res.Proto)
	}
	if res.GetHeader("Server") != "P1-Raw-Socket-Server" {
		t.Errorf("expected P1-Raw-Socket-Server, got %s", res.GetHeader("Server"))
	}
	if res.GetHeader("date") == "" {
		t.Errorf("expected non-empty Date header")
	}
}

func TestNewErrorResponse(t *testing.T) {
	sErr := NewStatusError(StatusNotFound, "page missing")
	res := NewErrorResponse(sErr)
	if res.StatusCode != 404 {
		t.Errorf("expected 404, got %d", res.StatusCode)
	}
	if res.GetHeader("Connection") != "close" {
		t.Errorf("expected Connection: close, got %s", res.GetHeader("Connection"))
	}
	if res.GetHeader("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("expected text/plain Content-Type, got %s", res.GetHeader("Content-Type"))
	}
	expectedBody := "404 Not Found: page missing\n"
	if string(res.Body) != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, string(res.Body))
	}
	if res.GetHeader("Content-Length") != "28" {
		t.Errorf("expected Content-Length 28, got %s", res.GetHeader("Content-Length"))
	}
}

func TestResponseHeaderAndBody(t *testing.T) {
	res := NewResponse(StatusOK)
	res.SetHeader("content-type", "application/json")
	res.SetHeader("Content-Type", "text/html")
	if res.GetHeader("CONTENT-TYPE") != "text/html" {
		t.Errorf("expected case-insensitive match for text/html, got %s", res.GetHeader("CONTENT-TYPE"))
	}

	body := []byte("<h1>Hello World</h1>")
	res.SetBody(body, "text/html; charset=utf-8")
	if string(res.Body) != string(body) {
		t.Errorf("expected body %q, got %q", string(body), string(res.Body))
	}
	if res.GetHeader("Content-Length") != "20" {
		t.Errorf("expected Content-Length 20, got %s", res.GetHeader("Content-Length"))
	}
}

func TestResponseSerialization(t *testing.T) {
	res := NewResponse(StatusOK)
	res.SetBody([]byte("test"), "text/plain")

	serialized := res.Serialize()
	str := string(serialized)
	if !strings.HasPrefix(str, "HTTP/1.1 200 OK\r\n") {
		t.Errorf("expected status line, got %s", str)
	}
	if !strings.HasSuffix(str, "\r\ntest") {
		t.Errorf("expected body at end, got %s", str)
	}

	// Test IsHeadOnly
	res.IsHeadOnly = true
	headSerialized := res.Serialize()
	headStr := string(headSerialized)
	if strings.Contains(headStr, "test") {
		t.Errorf("head-only response should not include body: %s", headStr)
	}
	if !strings.HasSuffix(headStr, "\r\n\r\n") {
		t.Errorf("head-only response should end in double CRLF")
	}
}

func TestChunkedEncoding(t *testing.T) {
	chunk := EncodeChunk([]byte("hello"))
	expected := "5\r\nhello\r\n"
	if string(chunk) != expected {
		t.Errorf("EncodeChunk expected %q, got %q", expected, string(chunk))
	}

	emptyChunk := EncodeChunk(nil)
	if emptyChunk != nil {
		t.Errorf("expected nil for empty data, got %v", emptyChunk)
	}

	finalChunk := EncodeFinalChunk()
	if string(finalChunk) != "0\r\n\r\n" {
		t.Errorf("EncodeFinalChunk expected 0\\r\\n\\r\\n, got %q", string(finalChunk))
	}
}

func TestChunkedDecoding(t *testing.T) {
	// Standard single and multi-chunk streams
	raw := []byte("5\r\nhello\r\n6\r\n world\r\n0\r\n\r\n")
	decoded, consumed, complete, err := DecodeChunked(raw)
	if err != nil {
		t.Fatalf("unexpected decode error: %v", err)
	}
	if !complete {
		t.Fatal("expected complete=true")
	}
	if string(decoded) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(decoded))
	}
	if consumed != len(raw) {
		t.Errorf("expected consumed=%d, got %d", len(raw), consumed)
	}

	// Chunk extensions
	rawExt := []byte("5;foo=bar\r\nhello\r\n0\r\n\r\n")
	decodedExt, _, completeExt, err := DecodeChunked(rawExt)
	if err != nil || !completeExt || string(decodedExt) != "hello" {
		t.Errorf("failed decoding chunk with extension: %v, %q", err, string(decodedExt))
	}

	// Pipelined data after chunked body
	rawPipelined := []byte("5\r\nhello\r\n0\r\n\r\nGET /next HTTP/1.1\r\n\r\n")
	_, consumedPipe, completePipe, err := DecodeChunked(rawPipelined)
	if err != nil || !completePipe {
		t.Fatalf("failed decoding pipelined chunked stream: %v", err)
	}
	expectedConsumed := len("5\r\nhello\r\n0\r\n\r\n")
	if consumedPipe != expectedConsumed {
		t.Errorf("expected consumed=%d, got %d", expectedConsumed, consumedPipe)
	}
	remaining := string(rawPipelined[consumedPipe:])
	if remaining != "GET /next HTTP/1.1\r\n\r\n" {
		t.Errorf("unexpected remaining pipelined bytes: %q", remaining)
	}

	// Incomplete chunk data
	rawIncomplete := []byte("5\r\nhel")
	_, consumedInc, completeInc, err := DecodeChunked(rawIncomplete)
	if err != nil {
		t.Fatalf("unexpected error for incomplete chunk: %v", err)
	}
	if completeInc || consumedInc != 0 {
		t.Errorf("expected incomplete=false and consumed=0, got complete=%v consumed=%d", completeInc, consumedInc)
	}

	// Malformed chunk size hex
	rawBadHex := []byte("xyz\r\nhello\r\n")
	_, _, _, err = DecodeChunked(rawBadHex)
	if err == nil {
		t.Fatal("expected error for invalid hex chunk size")
	}

	// Missing CRLF after chunk payload
	rawBadTrailing := []byte("5\r\nhelloXX0\r\n\r\n")
	_, _, _, err = DecodeChunked(rawBadTrailing)
	if err == nil {
		t.Fatal("expected error for missing CRLF after payload")
	}
}

func TestChunkedDecoder_Feed(t *testing.T) {
	decoder := NewChunkedDecoder()
	stream := []byte("4\r\nWiki\r\n5\r\npedia\r\n0\r\n\r\n")

	var allDecoded []byte
	var isComplete bool
	// Feed 3 bytes at a time
	for i := 0; i < len(stream); i += 3 {
		end := i + 3
		if end > len(stream) {
			end = len(stream)
		}
		part := stream[i:end]
		dec, comp, err := decoder.Feed(part)
		if err != nil {
			t.Fatalf("unexpected Feed error at index %d: %v", i, err)
		}
		allDecoded = append(allDecoded, dec...)
		if comp {
			isComplete = true
			break
		}
	}

	if !isComplete {
		t.Fatal("expected decoder to complete stream")
	}
	if string(allDecoded) != "Wikipedia" {
		t.Errorf("expected 'Wikipedia', got %q", string(allDecoded))
	}
}

func TestTCPFragmentation(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	accumulator := NewBufferAccumulator(serverConn)

	go func() {
		raw := []byte("GET /index.html HTTP/1.1\r\nHost: localhost\r\n\r\n")
		for _, b := range raw {
			_, _ = clientConn.Write([]byte{b})
			time.Sleep(1 * time.Millisecond)
		}
	}()

	req, err := accumulator.ReadNextRequest(5 * time.Second)
	if err != nil {
		t.Fatalf("unexpected error reading fragmented request: %v", err)
	}

	if req.Method != "GET" {
		t.Errorf("expected Method GET, got %s", req.Method)
	}
	if req.URI != "/index.html" {
		t.Errorf("expected URI /index.html, got %s", req.URI)
	}
}

func TestPipelinedRequests(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	accumulator := NewBufferAccumulator(serverConn)

	go func() {
		payload := []byte(
			"GET /first HTTP/1.1\r\nHost: test\r\n\r\n" +
				"POST /second HTTP/1.1\r\nHost: test\r\nContent-Length: 5\r\n\r\nHELLO",
		)
		_, _ = clientConn.Write(payload)
	}()

	req1, err := accumulator.ReadNextRequest(5 * time.Second)
	if err != nil {
		t.Fatalf("failed reading request 1: %v", err)
	}
	if req1.Method != "GET" || req1.URI != "/first" {
		t.Errorf("request 1 mismatch: got %s %s", req1.Method, req1.URI)
	}

	req2, err := accumulator.ReadNextRequest(5 * time.Second)
	if err != nil {
		t.Fatalf("failed reading request 2: %v", err)
	}
	if req2.Method != "POST" || req2.URI != "/second" {
		t.Errorf("request 2 mismatch: got %s %s", req2.Method, req2.URI)
	}
	if string(req2.Body) != "HELLO" {
		t.Errorf("expected body HELLO, got %q", string(req2.Body))
	}
}

func TestChunkedTransferDecoding(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	accumulator := NewBufferAccumulator(serverConn)

	go func() {
		fragments := [][]byte{
			[]byte("POST /chunked-test HTTP/1.1\r\nHost: localhost\r\nTransfer-Encoding: chunked\r\n\r\n"),
			[]byte("5\r\n"),
			[]byte("hello"),
			[]byte("\r\n6\r"),
			[]byte("\n world\r\n"),
			[]byte("0\r\n"),
			[]byte("\r\n"),
		}
		for _, frag := range fragments {
			_, _ = clientConn.Write(frag)
			time.Sleep(2 * time.Millisecond)
		}
	}()

	req, err := accumulator.ReadNextRequest(5 * time.Second)
	if err != nil {
		t.Fatalf("failed reading chunked request: %v", err)
	}
	if req.Method != "POST" || req.URI != "/chunked-test" {
		t.Errorf("unexpected request line: %s %s", req.Method, req.URI)
	}
	if !req.IsChunked {
		t.Errorf("expected IsChunked=true")
	}
	if string(req.Body) != "hello world" {
		t.Errorf("expected body 'hello world', got %q", string(req.Body))
	}
}

func TestThresholdViolations(t *testing.T) {
	t.Run("HeaderTooLarge", func(t *testing.T) {
		clientConn, serverConn := net.Pipe()
		defer clientConn.Close()
		defer serverConn.Close()

		accumulator := NewBufferAccumulator(serverConn)

		go func() {
			// Send more than MaxHeaderLength (8192) without delimiter
			bigData := make([]byte, MaxHeaderLength+100)
			for i := range bigData {
				bigData[i] = 'A'
			}
			_, _ = clientConn.Write(bigData)
		}()

		_, err := accumulator.ReadNextRequest(5 * time.Second)
		if err == nil {
			t.Fatal("expected error for header exceeding 8192 bytes")
		}
		var statusErr *StatusError
		if !errors.As(err, &statusErr) {
			t.Fatalf("expected *StatusError, got %T: %v", err, err)
		}
		if statusErr.Code != StatusHeaderTooLarge {
			t.Errorf("expected code 431 (StatusHeaderTooLarge), got %d", statusErr.Code)
		}
	})

	t.Run("URITooLong", func(t *testing.T) {
		clientConn, serverConn := net.Pipe()
		defer clientConn.Close()
		defer serverConn.Close()

		accumulator := NewBufferAccumulator(serverConn)

		go func() {
			longURI := "/" + strings.Repeat("a", MaxURILength+10)
			reqData := []byte("GET " + longURI + " HTTP/1.1\r\nHost: localhost\r\n\r\n")
			_, _ = clientConn.Write(reqData)
		}()

		_, err := accumulator.ReadNextRequest(5 * time.Second)
		if err == nil {
			t.Fatal("expected error for URI exceeding 2048 bytes")
		}
		var statusErr *StatusError
		if !errors.As(err, &statusErr) {
			t.Fatalf("expected *StatusError, got %T: %v", err, err)
		}
		if statusErr.Code != StatusURITooLong {
			t.Errorf("expected code 414 (StatusURITooLong), got %d", statusErr.Code)
		}
	})

	t.Run("HTTPVersionNotSupported", func(t *testing.T) {
		clientConn, serverConn := net.Pipe()
		defer clientConn.Close()
		defer serverConn.Close()

		accumulator := NewBufferAccumulator(serverConn)

		go func() {
			reqData := []byte("GET / HTTP/2.0\r\nHost: localhost\r\n\r\n")
			_, _ = clientConn.Write(reqData)
		}()

		_, err := accumulator.ReadNextRequest(5 * time.Second)
		if err == nil {
			t.Fatal("expected error for HTTP/2.0")
		}
		var statusErr *StatusError
		if !errors.As(err, &statusErr) {
			t.Fatalf("expected *StatusError, got %T: %v", err, err)
		}
		if statusErr.Code != StatusHTTPVersionNotSupported {
			t.Errorf("expected code 505 (StatusHTTPVersionNotSupported), got %d", statusErr.Code)
		}
	})

	t.Run("MalformedRequestLine", func(t *testing.T) {
		clientConn, serverConn := net.Pipe()
		defer clientConn.Close()
		defer serverConn.Close()

		accumulator := NewBufferAccumulator(serverConn)

		go func() {
			reqData := []byte("GET /\r\nHost: localhost\r\n\r\n")
			_, _ = clientConn.Write(reqData)
		}()

		_, err := accumulator.ReadNextRequest(5 * time.Second)
		if err == nil {
			t.Fatal("expected error for 2-token request line")
		}
		var statusErr *StatusError
		if !errors.As(err, &statusErr) {
			t.Fatalf("expected *StatusError, got %T: %v", err, err)
		}
		if statusErr.Code != StatusBadRequest {
			t.Errorf("expected code 400 (StatusBadRequest), got %d", statusErr.Code)
		}
	})
}

func TestBufferAccumulator_ResetAndBuffered(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	accumulator := NewBufferAccumulator(serverConn)
	if accumulator.Buffered() != 0 {
		t.Errorf("expected initially 0 buffered bytes, got %d", accumulator.Buffered())
	}

	go func() {
		// Send request with trailing bytes
		payload := []byte("GET / HTTP/1.1\r\nHost: test\r\n\r\nextra")
		_, _ = clientConn.Write(payload)
	}()

	req, err := accumulator.ReadNextRequest(5 * time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.URI != "/" {
		t.Errorf("expected URI '/', got %s", req.URI)
	}
	if accumulator.Buffered() != 5 {
		t.Errorf("expected 5 buffered bytes ('extra'), got %d", accumulator.Buffered())
	}

	accumulator.Reset()
	if accumulator.Buffered() != 0 {
		t.Errorf("expected 0 buffered bytes after Reset, got %d", accumulator.Buffered())
	}
}

func FuzzBufferAccumulator(f *testing.F) {
	f.Add([]byte("GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"))
	f.Add([]byte("POST /data HTTP/1.1\r\nContent-Length: 4\r\n\r\n1234"))
	f.Add([]byte("POST /chunk HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n4\r\ntest\r\n0\r\n\r\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		clientConn, serverConn := net.Pipe()
		defer clientConn.Close()
		defer serverConn.Close()

		acc := NewBufferAccumulator(serverConn)
		go func() {
			_, _ = clientConn.Write(data)
			_ = clientConn.Close()
		}()

		// Read should not panic under arbitrary bytes
		_, _ = acc.ReadNextRequest(50 * time.Millisecond)
	})
}

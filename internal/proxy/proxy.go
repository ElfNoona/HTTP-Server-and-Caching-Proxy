package proxy

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/http"
	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/proxy/cache"
)

// ProxyServer is an RFC-compliant HTTP/1.1 forward caching proxy operating on raw TCP sockets.
type ProxyServer struct {
	listener          *net.TCPListener
	defaultUpstream   string
	pool              *ConnectionPool
	cache             *cache.LRUCache
	downstreamTimeout time.Duration
	stopCh            chan struct{}
	closeOnce         sync.Once
}

// NewProxyServer instantiates a ProxyServer bound to the given listener with pooling and LRU caching.
func NewProxyServer(listener *net.TCPListener, defaultUpstream string, lruCache *cache.LRUCache) *ProxyServer {
	return &ProxyServer{
		listener:          listener,
		defaultUpstream:   defaultUpstream,
		pool:              NewConnectionPool(32, 10*time.Second, 2*time.Second),
		cache:             lruCache,
		downstreamTimeout: 5 * time.Second,
		stopCh:            make(chan struct{}),
	}
}

// Stop initiates a graceful shutdown of the proxy listener, connection queues, and pooled sockets.
func (p *ProxyServer) Stop() {
	p.closeOnce.Do(func() {
		close(p.stopCh)
		if p.listener != nil {
			_ = p.listener.Close()
		}
		if p.pool != nil {
			p.pool.CloseAll()
		}
	})
}

// Start begins accepting downstream connections and dispatches them across a worker pool.
func (p *ProxyServer) Start(numWorkers int) {
	if numWorkers <= 0 {
		numWorkers = 64
	}

	connQueue := make(chan net.Conn, numWorkers*16)
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for conn := range connQueue {
				p.handleDownstream(conn)
			}
		}()
	}

	for {
		select {
		case <-p.stopCh:
			_ = p.listener.Close()
			close(connQueue)
			p.pool.CloseAll()
			wg.Wait()
			return
		default:
		}

		_ = p.listener.SetDeadline(time.Now().Add(1 * time.Second))
		conn, err := p.listener.AcceptTCP()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-p.stopCh:
				close(connQueue)
				p.pool.CloseAll()
				wg.Wait()
				return
			default:
				continue
			}
		}

		select {
		case connQueue <- conn:
		case <-p.stopCh:
			_ = conn.Close()
			_ = p.listener.Close()
			close(connQueue)
			p.pool.CloseAll()
			wg.Wait()
			return
		}
	}
}

// handleDownstream processes persistent HTTP/1.1 client requests, evaluating cache hits and forwarding misses.
func (p *ProxyServer) handleDownstream(clientConn net.Conn) {
	defer clientConn.Close()

	acc := http.NewBufferAccumulator(clientConn)

	for {
		req, err := acc.ReadNextRequest(p.downstreamTimeout)
		if err != nil {
			if err == io.EOF {
				return
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				return
			}
			if errors.Is(err, os.ErrDeadlineExceeded) || strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline exceeded") {
				return
			}
			if statusErr, ok := err.(*http.StatusError); ok {
				errRes := http.NewErrorResponse(statusErr)
				_, _ = errRes.WriteTo(clientConn)
				return
			}
			return
		}

		// 2. Extract target host and relative path
		targetHost, targetPath, err := ExtractUpstreamTarget(req, p.defaultUpstream)
		if err != nil {
			errRes := http.NewErrorResponse(http.NewStatusError(http.StatusBadRequest, "Invalid Target URI"))
			_, _ = errRes.WriteTo(clientConn)
			return
		}

		// 3. Build cache key: METHOD + " " + targetPath
		cacheKey := req.Method + " " + targetPath

		// 4. Cache Lookup
		entry, hit := p.cache.Get(cacheKey)
		if hit && entry != nil {
			// Check conditional validation (ETag / If-Modified-Since)
			is304 := cache.CheckConditionalValidation(req.Headers, entry)
			if is304 {
				res := http.NewResponse(http.StatusNotModified)
				for k, v := range entry.CloneHeaders() {
					res.SetHeader(k, v)
				}
				res.SetHeader("X-Cache", "HIT")
				if req.Close {
					res.SetHeader("Connection", "close")
				} else {
					res.SetHeader("Connection", "keep-alive")
				}
				_, _ = res.WriteTo(clientConn)
				fmt.Printf("[PROXY] [CACHE HIT 304] %s %s\n", req.Method, targetPath)
				if req.Close {
					return
				}
				continue
			}

			// Cache HIT 200
			res := http.NewResponse(entry.StatusCode)
			for k, v := range entry.CloneHeaders() {
				res.SetHeader(k, v)
			}
			res.SetHeader("X-Cache", "HIT")
			if req.Close {
				res.SetHeader("Connection", "close")
			} else {
				res.SetHeader("Connection", "keep-alive")
			}
			res.Body = entry.Body
			_, _ = res.WriteTo(clientConn)
			fmt.Printf("[PROXY] [CACHE HIT 200] %s %s\n", req.Method, targetPath)
			if req.Close {
				return
			}
			continue
		}

		// 5. Cache Miss - Forward to Upstream
		fmt.Printf("[PROXY] [CACHE MISS] %s %s -> Upstream %s\n", req.Method, targetPath, targetHost)
		upConn, err := p.pool.Acquire(targetHost)
		if err != nil {
			errRes := http.NewErrorResponse(http.NewStatusError(502, "Bad Gateway: Cannot Connect to Upstream"))
			_, _ = errRes.WriteTo(clientConn)
			return
		}

		sanitizedHeaders := SanitizeHeaders(req.Headers)
		rawReq := FormatUpstreamRequest(req, targetHost, targetPath, sanitizedHeaders)

		_ = upConn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, writeErr := upConn.Write(rawReq); writeErr != nil {
			p.pool.Release(targetHost, upConn, true)
			errRes := http.NewErrorResponse(http.NewStatusError(502, "Bad Gateway: Write Failed"))
			_, _ = errRes.WriteTo(clientConn)
			return
		}

		upAcc := http.NewBufferAccumulator(upConn)
		upRes, readErr := upAcc.ReadNextResponse(5 * time.Second)
		if readErr != nil {
			p.pool.Release(targetHost, upConn, true)
			errRes := http.NewErrorResponse(http.NewStatusError(502, "Bad Gateway: Read Failed"))
			_, _ = errRes.WriteTo(clientConn)
			return
		}

		// Check if upstream socket should be closed
		upClose := false
		if connHdr, ok := upRes.Headers["connection"]; ok && strings.EqualFold(connHdr, "close") {
			upClose = true
		}
		p.pool.Release(targetHost, upConn, upClose)

		// 6. Store to Cache if cacheable
		if cache.IsCacheable(req.Method, upRes.StatusCode, upRes.Headers) {
			ttl, _, _ := cache.ParseCacheControl(upRes.Headers["cache-control"])
			etag := upRes.Headers["etag"]
			if etag == "" && len(upRes.Body) > 0 {
				etag = cache.ComputeETag(upRes.Body)
				upRes.SetHeader("ETag", etag)
			}
			lastMod := upRes.Headers["last-modified"]

			cacheEntry := &cache.CacheEntry{
				Key:          cacheKey,
				StatusCode:   upRes.StatusCode,
				Headers:      upRes.Headers,
				Body:         upRes.Body,
				CreatedAt:    time.Now(),
				TTL:          ttl,
				ETag:         etag,
				LastModified: lastMod,
			}
			cacheEntry.EnsureSize()
			p.cache.Set(cacheEntry)
		}

		// 7. Forward to Downstream
		upRes.SetHeader("X-Cache", "MISS")
		if req.Close {
			upRes.SetHeader("Connection", "close")
		} else {
			upRes.SetHeader("Connection", "keep-alive")
		}

		if _, writeErr := upRes.WriteTo(clientConn); writeErr != nil {
			return
		}

		if req.Close {
			return
		}
	}
}


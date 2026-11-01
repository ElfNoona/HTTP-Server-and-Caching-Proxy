package proxy

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// UpstreamConn wraps an idle persistent TCP connection to an upstream host.
type UpstreamConn struct {
	conn     *net.TCPConn
	target   string
	lastUsed time.Time
}

// ConnectionPool manages reusable persistent TCP connections to upstream origin backends.
type ConnectionPool struct {
	conns          map[string][]*UpstreamConn
	maxIdlePerHost int
	idleTimeout    time.Duration
	dialTimeout    time.Duration
	mu             sync.Mutex
}

// NewConnectionPool initializes an upstream connection pool with specified pooling and timeout parameters.
func NewConnectionPool(maxIdlePerHost int, idleTimeout, dialTimeout time.Duration) *ConnectionPool {
	if maxIdlePerHost <= 0 {
		maxIdlePerHost = 32
	}
	if idleTimeout <= 0 {
		idleTimeout = 10 * time.Second
	}
	if dialTimeout <= 0 {
		dialTimeout = 2 * time.Second
	}

	return &ConnectionPool{
		conns:          make(map[string][]*UpstreamConn),
		maxIdlePerHost: maxIdlePerHost,
		idleTimeout:    idleTimeout,
		dialTimeout:    dialTimeout,
	}
}

// Acquire retrieves an active, unexpired idle connection for targetAddr, or dials a new raw TCP connection.
func (p *ConnectionPool) Acquire(targetAddr string) (*net.TCPConn, error) {
	p.mu.Lock()
	now := time.Now()

	for {
		list, ok := p.conns[targetAddr]
		if !ok || len(list) == 0 {
			break
		}

		// Pop the most recent idle connection (LIFO order maximizes socket freshness)
		lastIdx := len(list) - 1
		uConn := list[lastIdx]
		p.conns[targetAddr] = list[:lastIdx]

		// Check if connection has exceeded idle timeout
		if now.Sub(uConn.lastUsed) > p.idleTimeout {
			_ = uConn.conn.Close()
			continue
		}

		p.mu.Unlock()
		return uConn.conn, nil
	}
	p.mu.Unlock()

	// Dial a new raw TCP socket
	rawConn, err := net.DialTimeout("tcp", targetAddr, p.dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to dial upstream %s: %w", targetAddr, err)
	}

	tcpConn, ok := rawConn.(*net.TCPConn)
	if !ok {
		_ = rawConn.Close()
		return nil, fmt.Errorf("connection to %s is not *net.TCPConn", targetAddr)
	}

	return tcpConn, nil
}

// Release returns a connection to the pool or closes it if closeConn is true or capacity is exceeded.
func (p *ConnectionPool) Release(targetAddr string, conn *net.TCPConn, closeConn bool) {
	if conn == nil {
		return
	}

	if closeConn {
		_ = conn.Close()
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	list := p.conns[targetAddr]
	now := time.Now()

	// Purge any expired connections from the back
	cleaned := make([]*UpstreamConn, 0, len(list))
	for _, c := range list {
		if now.Sub(c.lastUsed) <= p.idleTimeout {
			cleaned = append(cleaned, c)
		} else {
			_ = c.conn.Close()
		}
	}

	if len(cleaned) < p.maxIdlePerHost {
		cleaned = append(cleaned, &UpstreamConn{
			conn:     conn,
			target:   targetAddr,
			lastUsed: now,
		})
		p.conns[targetAddr] = cleaned
	} else {
		// Pool full: close socket to prevent descriptor exhaustion
		_ = conn.Close()
		p.conns[targetAddr] = cleaned
	}
}

// CloseAll terminates and cleans up all active pooled connections across all hosts.
func (p *ConnectionPool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for host, list := range p.conns {
		for _, c := range list {
			_ = c.conn.Close()
		}
		delete(p.conns, host)
	}
}


package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/proxy"
	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/proxy/cache"
)

func main() {
	port := flag.Int("port", 9090, "Port to bind the raw-socket forward caching proxy")
	defaultUpstream := flag.String("upstream", "localhost:8080", "Default upstream origin host:port")
	cacheSize := flag.Int64("cache-size", 52428800, "Maximum in-memory LRU cache capacity in bytes (default 50MB)")
	workers := flag.Int("workers", 64, "Number of worker goroutines for downstream client connections")
	flag.Parse()

	fmt.Println("==================================================")
	fmt.Println("  P1 Raw-Socket HTTP/1.1 Forward Caching Proxy")
	fmt.Printf("  Listen Port:      %d\n", *port)
	fmt.Printf("  Default Upstream: %s\n", *defaultUpstream)
	fmt.Printf("  Cache Capacity:   %d bytes (%.2f MB)\n", *cacheSize, float64(*cacheSize)/(1024*1024))
	fmt.Printf("  Workers:          %d\n", *workers)
	fmt.Println("==================================================")

	// Initialize in-memory LRU cache
	lruCache := cache.NewLRUCache(*cacheSize)

	// Resolve and bind raw TCP listener
	addrStr := fmt.Sprintf(":%d", *port)
	laddr, err := net.ResolveTCPAddr("tcp", addrStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to resolve TCP address %s: %v\n", addrStr, err)
		os.Exit(1)
	}

	listener, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to listen on TCP address %s: %v\n", addrStr, err)
		os.Exit(1)
	}
	defer listener.Close()

	proxyServer := proxy.NewProxyServer(listener, *defaultUpstream, lruCache)
	fmt.Printf("[PROXY] Forwarding proxy listening on %s (Upstream: %s)\n", listener.Addr().String(), *defaultUpstream)

	// Trap OS interrupt signals for graceful teardown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	serverDone := make(chan struct{})
	go func() {
		proxyServer.Start(*workers)
		close(serverDone)
	}()

	sig := <-sigCh
	fmt.Printf("\n[PROXY] Received signal %v. Initiating graceful shutdown...\n", sig)
	proxyServer.Stop()

	<-serverDone
	fmt.Println("[PROXY] Proxy server stopped cleanly.")
}


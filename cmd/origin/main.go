package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/origin"
	"github.com/ElfNoona/HTTP-Server-and-Caching-Proxy/internal/origin/runtimes"
)

func main() {
	port := flag.Int("port", 8080, "Port to bind the raw-socket origin server")
	runtimeMode := flag.String("runtime", "pool", "Concurrency runtime to use: 'pool' or 'goroutine'")
	workers := flag.Int("workers", 64, "Number of worker goroutines for pool mode")
	queueSize := flag.Int("queue", 1000, "Connection queue size for pool mode")
	publicDir := flag.String("dir", "./public", "Directory containing public static assets")
	flag.Parse()

	fmt.Println("==================================================")
	fmt.Println("  P1 Raw-Socket HTTP/1.1 Origin Server")
	fmt.Printf("  Port:     %d\n", *port)
	fmt.Printf("  Runtime:  %s\n", *runtimeMode)
	if *runtimeMode == "pool" {
		fmt.Printf("  Workers:  %d\n", *workers)
		fmt.Printf("  Queue:    %d\n", *queueSize)
	}
	fmt.Printf("  Assets:   %s\n", *publicDir)
	fmt.Println("==================================================")

	// Initialize StaticDispatcher; fail fast if the directory is invalid
	dispatcher, err := origin.NewStaticDispatcher(*publicDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to initialize static dispatcher: %v\n", err)
		os.Exit(1)
	}

	// Select concurrency runtime
	var runtime origin.ConcurrencyRuntime
	switch *runtimeMode {
	case "pool":
		runtime = runtimes.NewWorkerPool(*workers, *queueSize)
	case "goroutine":
		runtime = runtimes.NewGoroutineRuntime()
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid runtime %q. Must be 'pool' or 'goroutine'\n", *runtimeMode)
		flag.Usage()
		os.Exit(1)
	}

	// Resolve address and bind raw TCP listener
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

	fmt.Printf("[ORIGIN] Server listening on %s (Mode: %s)\n", listener.Addr().String(), *runtimeMode)

	// Graceful shutdown handling
	stopCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runtime.Run(listener, dispatcher, stopCh)
	}()

	// Block until interrupt signal received
	sig := <-sigCh
	fmt.Printf("\n[ORIGIN] Received signal %v. Initiating graceful shutdown...\n", sig)
	close(stopCh)

	if err := <-serverDone; err != nil {
		fmt.Fprintf(os.Stderr, "[ORIGIN] Runtime exited with error: %v\n", err)
	}

	fmt.Println("[ORIGIN] Server stopped cleanly.")
}


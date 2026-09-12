// Command demo runs synthetic local fixtures only. It never connects to the platform.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"starling/internal/demoweb"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:34115", "Loopback address only")
	dir := flag.String("dir", "frontend/dist", "Built frontend directory")
	flag.Parse()
	host, _, e := net.SplitHostPort(*addr)
	if e != nil || host != "127.0.0.1" {
		log.Fatal("demo must bind 127.0.0.1")
	}
	d, e := demoweb.New(*dir, *addr)
	if e != nil {
		log.Fatal("demo initialization failed")
	}
	defer d.Close()
	server := &http.Server{Addr: *addr, Handler: d, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(c)
	}()
	fmt.Printf("SYNTHETIC DEMO ONLY — http://%s\n", *addr)
	if e = server.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Fatal("demo server failed")
	}
}

// Command evernote-lite is the entry point for the Evernote-Lite notes API.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PalashSinha14/evernote/internal/app"
	"github.com/PalashSinha14/evernote/internal/config"
)

const (
	startupTimeout  = 15 * time.Second
	shutdownTimeout = 10 * time.Second
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	startCtx, cancelStart := context.WithTimeout(context.Background(), startupTimeout)
	defer cancelStart()

	application, err := app.New(startCtx, cfg)
	if err != nil {
		log.Fatalf("startup failed: %v", err)
	}

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           application.Router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// The server runs in its own goroutine so that main can wait on a
	// termination signal and shut down in an orderly way.
	go func() {
		log.Printf("evernote-lite listening on :%s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down")

	stopCtx, cancelStop := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelStop()

	// Stop accepting new requests and let in-flight ones finish before the
	// database connection is closed underneath them.
	if err := server.Shutdown(stopCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
	if err := application.Close(stopCtx); err != nil {
		log.Printf("closing database: %v", err)
	}
	log.Println("stopped")
}

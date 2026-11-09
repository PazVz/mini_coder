package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/PazVz/mini_coder/internal/auth"
	"github.com/PazVz/mini_coder/internal/database"
	"github.com/PazVz/mini_coder/internal/httpapi"
)

// Release builds set version through the Go linker's -X flag.
var version = "dev"

func main() {
	defaultAddr := os.Getenv("MINICODER_ADDR")
	if defaultAddr == "" {
		defaultAddr = "127.0.0.1:8080"
	}
	addr := flag.String("addr", defaultAddr, "HTTP listen address")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *addr, os.Getenv("MINICODER_DATABASE_URL"), logger); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, addr, databaseURL string, logger *slog.Logger) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	defer listener.Close()
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	secure := true
	if value := os.Getenv("MINICODER_COOKIE_SECURE"); value != "" {
		secure, err = strconv.ParseBool(value)
		if err != nil {
			return errors.New("MINICODER_COOKIE_SECURE must be a boolean")
		}
	}
	authService, err := auth.NewService(pool, os.Getenv("MINICODER_SETUP_TOKEN"))
	if err != nil {
		return err
	}
	cleanupCtx, stopCleanup := context.WithCancel(ctx)
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-cleanupCtx.Done():
				return
			case <-ticker.C:
				queryCtx, cancel := context.WithTimeout(cleanupCtx, 30*time.Second)
				cleanupErr := authService.DeleteExpiredSessions(queryCtx)
				cancel()
				if cleanupErr != nil && cleanupCtx.Err() == nil {
					logger.Error("clean up expired sessions", "error", cleanupErr)
				}
			}
		}
	}()
	// Stop and join the cleanup worker before the deferred pool.Close runs.
	defer func() {
		stopCleanup()
		<-cleanupDone
	}()

	server := &http.Server{
		Handler: httpapi.NewHandler(httpapi.Config{
			Auth:         authService,
			Logger:       logger,
			CookieSecure: secure,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	logger.Info("server listening", "address", listener.Addr().String(), "version", version)
	return serve(ctx, server, listener, logger)
}

func serve(ctx context.Context, server *http.Server, listener net.Listener, logger *slog.Logger) error {
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.Serve(listener)
	}()

	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		logger.Info("server shutting down")
	}

	// The signal context is already canceled; shutdown needs its own deadline.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	logger.Info("server stopped")
	return nil
}

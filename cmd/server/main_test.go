package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestRunReportsListenError(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := run(context.Background(), "invalid-address", logger); err == nil {
		t.Fatal("run succeeded with an invalid listen address")
	}
}

func TestServeFinishesActiveRequestBeforeShutdown(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	started := make(chan struct{})
	finishRequest := make(chan struct{})
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(started)
			<-finishRequest
			_, _ = io.WriteString(w, "finished")
		}),
	}
	// Unblock the handler before closing the server, even when an assertion fails.
	t.Cleanup(func() { _ = server.Close() })
	t.Cleanup(func() {
		select {
		case <-finishRequest:
		default:
			close(finishRequest)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	shutdownStarted := make(chan struct{})
	server.RegisterOnShutdown(func() { close(shutdownStarted) })
	serveDone := make(chan error, 1)
	go func() {
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		serveDone <- serve(ctx, server, listener, logger)
	}()

	responseDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Get("http://" + listener.Addr().String())
		if err != nil {
			responseDone <- err
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err == nil && string(body) != "finished" {
			err = fmt.Errorf("response body = %q, want finished", body)
		}
		responseDone <- err
	}()

	waitForSignal(t, started, "request to start")
	cancel()
	waitForSignal(t, shutdownStarted, "shutdown to start")
	select {
	case err := <-serveDone:
		t.Fatalf("server exited before the request completed: %v", err)
	default:
	}
	close(finishRequest)
	for _, result := range []<-chan error{responseDone, serveDone} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for graceful shutdown")
		}
	}
}

func waitForSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

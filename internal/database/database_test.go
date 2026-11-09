package database_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/PazVz/mini_coder/internal/database"
)

func TestOpenRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()
	for _, url := range []string{"", "postgres://alice:private-password@localhost/db?pool_max_conns=invalid"} {
		pool, err := database.Open(t.Context(), url)
		if err == nil {
			pool.Close()
			t.Fatal("expected configuration error")
		}
		if strings.Contains(err.Error(), "private-password") {
			t.Fatal("configuration error exposed database password")
		}
	}
}

func TestOpenHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	pool, err := database.Open(ctx, "postgres://minicoder@127.0.0.1:5432/minicoder?sslmode=disable")
	if err == nil {
		pool.Close()
		t.Fatal("expected cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

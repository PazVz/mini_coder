package main

import (
	"io"
	"strings"
	"testing"
)

func TestInvalidCommandDoesNotConnect(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{nil, {"reset"}, {"up", "down"}} {
		if err := run(t.Context(), args, "", io.Discard); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Fatalf("run(%v) error = %v, want usage error", args, err)
		}
	}
}

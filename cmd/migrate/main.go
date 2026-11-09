package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PazVz/mini_coder/internal/database"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv("MINICODER_DATABASE_URL"), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, databaseURL string, out io.Writer) error {
	if len(args) != 1 || (args[0] != "up" && args[0] != "down" && args[0] != "status") {
		return fmt.Errorf("usage: mini-coder-migrate <up|down|status>")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	provider, err := database.NewMigrator(pool)
	if err != nil {
		return err
	}
	defer provider.Close()
	switch args[0] {
	case "up":
		results, err := provider.Up(ctx)
		if err != nil {
			return fmt.Errorf("migrate up: %w", err)
		}
		_, err = fmt.Fprintf(out, "Applied %d migration(s)\n", len(results))
		return err
	case "down":
		result, err := provider.Down(ctx)
		if err != nil {
			return fmt.Errorf("migrate down: %w", err)
		}
		_, err = fmt.Fprintf(out, "Rolled back migration %d\n", result.Source.Version)
		return err
	case "status":
		statuses, err := provider.Status(ctx)
		if err != nil {
			return fmt.Errorf("migration status: %w", err)
		}
		for _, status := range statuses {
			if _, err := fmt.Fprintf(out, "%d\t%s\t%s\n", status.Source.Version, status.State, status.Source.Path); err != nil {
				return err
			}
		}
	}
	return nil
}

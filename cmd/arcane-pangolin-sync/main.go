package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/GitLogos/arcane-pangolin-sync/internal/syncer"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := syncer.LoadConfig()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := syncer.New(cfg, logger)
	if err := app.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("syncer stopped", "error", err)
		os.Exit(1)
	}
}

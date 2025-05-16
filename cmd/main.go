package main

import (
	"ararauna/internal/command"
	"ararauna/internal/config"
	"ararauna/internal/logger"
	"ararauna/internal/metrics"
	"ararauna/internal/storage"
	"ararauna/internal/toolbox"
	"ararauna/internal/transport"
	"ararauna/internal/wal"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
)

func main() {
	configPath := flag.String("config", "config.yml", "path to YAML config")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config load:", err)
		os.Exit(1)
	}

	log, err := logger.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "logger init:", err)
		os.Exit(1)
	}
	defer func() { _ = log.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	tb := toolbox.New(cfg, log)

	metricsRec, err := metrics.New(cfg, log)
	if err != nil {
		log.Error("metrics init", zap.Error(err))
		return
	}
	tb.Metrics = metricsRec

	walWriter, err := wal.New(tb)
	if err != nil {
		log.Error("wal init", zap.Error(err))
		return
	}

	store, err := storage.New(ctx, tb, walWriter)
	if err != nil {
		log.Error("storage init", zap.Error(err))
		_ = walWriter.Close()
		return
	}

	handler := command.New(tb, store)
	server := transport.New(tb, handler)

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		if err := server.Start(ctx); err != nil {
			log.Error("server", zap.Error(err))
		}
	}()

	select {
	case <-ctx.Done():
		log.Info("shutdown signal received, draining connections")
	case <-serverDone:
		log.Warn("server stopped unexpectedly")
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancelShutdown()

	select {
	case <-serverDone:
		log.Info("server drained")
	case <-shutdownCtx.Done():
		log.Warn("shutdown timeout exceeded, forcing close",
			zap.Duration("timeout", cfg.Server.ShutdownTimeout))
	}

	if err := walWriter.Close(); err != nil {
		log.Error("wal close", zap.Error(err))
	}

	if err := metricsRec.Close(); err != nil {
		log.Error("metrics close", zap.Error(err))
	}

	log.Info("shutdown complete")
}

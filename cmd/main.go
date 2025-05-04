package main

import (
	"ararauna/internal/command"
	"ararauna/internal/config"
	"ararauna/internal/logger"
	"ararauna/internal/storage"
	"ararauna/internal/toolbox"
	"ararauna/internal/transport"
	"context"
	"flag"
	stdlibLog "log"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
)

func main() {
	configPath := flag.String("config", "config.yml", "path to YAML config")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(*configPath)
	if err != nil {
		stdlibLog.Fatalf("config load: %v", err)
	}

	log, err := logger.New(cfg)
	if err != nil {
		stdlibLog.Fatalf("logger init: %v", err)
	}
	defer log.Sync()

	tb := toolbox.New(cfg, log)

	store := storage.New(ctx, tb)
	commandHandler := command.New(tb, store)
	tcpServer := transport.New(tb, commandHandler)

	if err := tcpServer.Start(ctx); err != nil {
		log.Fatal("server error", zap.Error(err))
	}
}

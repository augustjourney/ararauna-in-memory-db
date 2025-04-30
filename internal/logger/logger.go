package logger

import (
	"ararauna/internal/config"
	"errors"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func New(cfg *config.Config) (*zap.Logger, error) {
	if cfg.Logger.FilePath == "" {
		return nil, errors.New("logger: file_path is required")
	}

	levelStr := cfg.Logger.Level
	if levelStr == "" {
		levelStr = "info"
	}
	level, err := zapcore.ParseLevel(levelStr)
	if err != nil {
		return nil, err
	}

	zcfg := zap.NewProductionConfig()
	zcfg.Level = zap.NewAtomicLevelAt(level)
	zcfg.OutputPaths = []string{"stdout", cfg.Logger.FilePath}
	zcfg.ErrorOutputPaths = []string{"stderr", cfg.Logger.FilePath}

	return zcfg.Build()
}

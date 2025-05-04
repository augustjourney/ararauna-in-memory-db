package logger

import (
	"ararauna/internal/config"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func New(cfg *config.Config) (*zap.Logger, error) {
	level, err := zapcore.ParseLevel(cfg.Logger.Level)
	if err != nil {
		return nil, err
	}

	zcfg := zap.NewProductionConfig()
	zcfg.Level = zap.NewAtomicLevelAt(level)
	zcfg.OutputPaths = []string{"stdout"}
	zcfg.ErrorOutputPaths = []string{"stderr"}
	if cfg.Logger.FilePath != "" {
		zcfg.OutputPaths = append(zcfg.OutputPaths, cfg.Logger.FilePath)
		zcfg.ErrorOutputPaths = append(zcfg.ErrorOutputPaths, cfg.Logger.FilePath)
	}

	return zcfg.Build()
}

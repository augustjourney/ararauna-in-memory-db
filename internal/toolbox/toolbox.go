package toolbox

import (
	"ararauna/internal/config"
	"ararauna/internal/metrics"

	"go.uber.org/zap"
)

type Toolbox struct {
	Cfg     *config.Config
	Logger  *zap.Logger
	Metrics *metrics.Recorder
}

func New(cfg *config.Config, logger *zap.Logger) *Toolbox {
	return &Toolbox{
		Cfg:    cfg,
		Logger: logger,
	}
}

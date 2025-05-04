package toolbox

import (
	"ararauna/internal/config"

	"go.uber.org/zap"
)

type Toolbox struct {
	Cfg    *config.Config
	Logger *zap.Logger
}

func New(cfg *config.Config, logger *zap.Logger) *Toolbox {
	return &Toolbox{
		Cfg:    cfg,
		Logger: logger,
	}
}

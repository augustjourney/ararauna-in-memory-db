package toolbox

import (
	"ararauna/internal/config"

	"go.uber.org/zap"
)

type Toolbox struct {
	Cfg    *config.Config
	Logger *zap.Logger
}

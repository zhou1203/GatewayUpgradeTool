package options

import (
	"github.com/zhou1203/GatewayUpgradeTool/pkg/options"
)

type RunOptions struct {
	options.Options
	SpecificAppVersion string
	NeedBackup         bool
}

func NewRunOptions() *RunOptions {
	return &RunOptions{}
}

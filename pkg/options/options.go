package options

type Options struct {
	KubeConfigPath string
	GatewayNames   string
	BackupDir      string
}

func NewRunOptions() *Options {
	return &Options{}
}

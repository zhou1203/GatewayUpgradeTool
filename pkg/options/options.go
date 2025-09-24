package options

type Options struct {
	KubeConfigPath string
	GatewayNames   string
	Backup         *BackupOptions
}

type BackupOptions struct {
	Enabled bool
	Dir     string
}

func NewOptions() *Options {
	return &Options{
		GatewayNames: "",
		Backup:       &BackupOptions{Enabled: false, Dir: ""},
	}
}

package options

import "flag"

type RunOptions struct {
	KubeConfig   string
	GatewayNames string
	AppVersion   string
	NeedBackup   bool
	BackupDir    string
}

func NewRunOptions() *RunOptions {
	return &RunOptions{}
}

func ParseFlags() *RunOptions {
	opts := NewRunOptions()
	flag.StringVar(&opts.KubeConfig, "kubeconfig", "", "Path to the kubeconfig file ")
	flag.StringVar(&opts.GatewayNames, "gateways", "", "Comma-separated list of gateway names to upgrade")
	flag.StringVar(&opts.AppVersion, "app-version", "", "App version")
	flag.BoolVar(&opts.NeedBackup, "need-backup", false, "Need backup")
	flag.StringVar(&opts.BackupDir, "backup-dir", "/mnt/backup", "Backup directory")
	flag.Parse()
	return opts
}

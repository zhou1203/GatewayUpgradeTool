package options

import "flag"

type RunOptions struct {
	KubeConfig   string
	GatewayNames string
	AppVersion   string
}

func NewRunOptions() *RunOptions {
	return &RunOptions{}
}

func ParseFlags() *RunOptions {
	opts := NewRunOptions()
	flag.StringVar(&opts.KubeConfig, "kubeconfig", ".kube/config", "Path to the kubeconfig file ")
	flag.StringVar(&opts.GatewayNames, "gateways", "", "Comma-separated list of gateway names to upgrade")
	flag.StringVar(&opts.AppVersion, "app-version", "", "App version")
	flag.Parse()
	return opts
}

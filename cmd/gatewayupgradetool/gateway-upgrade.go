package main

import (
	"github.com/zhou1203/GatewayUpgradeTool/cmd/gatewayupgradetool/options"

	"github.com/zhou1203/GatewayUpgradeTool/pkg/runner"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

func main() {
	opts := options.ParseFlags()

	newRunner, err := runner.NewRunner(opts)
	if err != nil {
		klog.Fatalf("failed to parse flags, %v", err)
		return
	}
	newRunner.Run(signals.SetupSignalHandler())
}

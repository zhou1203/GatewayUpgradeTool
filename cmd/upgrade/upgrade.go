package upgrade

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zhou1203/GatewayUpgradeTool/cmd/upgrade/options"

	"github.com/zhou1203/GatewayUpgradeTool/pkg/upgrade"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

var opts = options.NewRunOptions()

var Cmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade the gateway",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("🚀 Starting to upgrade...")
		newRunner, err := upgrade.NewRunner(opts)
		if err != nil {
			return fmt.Errorf("failed to init runner, %v", err)
		}
		err = newRunner.Run(signals.SetupSignalHandler())
		if err != nil {
			return fmt.Errorf("failed to upgrade, %v", err)
		}
		return nil
	},
}

func init() {
	Cmd.Flags().StringVar(&opts.KubeConfigPath, "kubeconfig", "", "Path to the kubeconfig file ")
	Cmd.Flags().StringVar(&opts.GatewayNames, "gateways", "", "Comma-separated list of gateway names to upgrade")
	Cmd.Flags().StringVar(&opts.SpecificAppVersion, "specific-app-version", "", "App version")
	Cmd.Flags().BoolVar(&opts.Backup.Enabled, "backup-enabled", false, "Need backup")
	Cmd.Flags().StringVar(&opts.Backup.Dir, "backup-dir", "/mnt/backup", "Backup directory")
}

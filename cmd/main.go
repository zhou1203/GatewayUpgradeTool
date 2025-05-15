package main

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/zhou1203/GatewayUpgradeTool/cmd/rollback"
	"github.com/zhou1203/GatewayUpgradeTool/cmd/upgrade"
	"os"
)

var rootCmd = &cobra.Command{
	Use:   "gateway-tool",
	Short: "Gateway upgrade tool for managing gateway versions",
	Long:  `A CLI tool to upgrade or rollback gateway components.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	// 注册子命令
	rootCmd.AddCommand(upgrade.Cmd)
	rootCmd.AddCommand(rollback.Cmd)
}

func main() {
	Execute()
}

package rollback

import (
	"fmt"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "rollback",
	Short: "Rollback the gateway to the previous version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🔙 Starting to rollback...")
		// 你可以在这里添加实际的回滚逻辑
	},
}

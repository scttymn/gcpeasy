package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View pod logs (shortcut for 'pod logs')",
	Long:  "View logs from application pods. This is a shortcut for 'gcpeasy pod logs'.",
	Run: func(cmd *cobra.Command, args []string) {
		follow, _ := cmd.Flags().GetBool("follow")
		errorOnly, _ := cmd.Flags().GetBool("error")
		warnOnly, _ := cmd.Flags().GetBool("warn")
		infoOnly, _ := cmd.Flags().GetBool("info")
		debugOnly, _ := cmd.Flags().GetBool("debug")
		
		var level string
		if errorOnly {
			level = "error"
		} else if warnOnly {
			level = "warn"
		} else if infoOnly {
			level = "info"
		} else if debugOnly {
			level = "debug"
		}
		
		if err := runPodLogs(follow, level); err != nil {
			fmt.Printf("Error viewing logs: %v\n", err)
		}
	},
}

func init() {
	logsCmd.Flags().BoolP("follow", "f", false, "Follow logs in real-time")
	logsCmd.Flags().BoolP("error", "e", false, "Show only error logs")
	logsCmd.Flags().BoolP("warn", "w", false, "Show only warning logs")
	logsCmd.Flags().BoolP("info", "i", false, "Show only info logs")
	logsCmd.Flags().BoolP("debug", "d", false, "Show only debug logs")
	rootCmd.AddCommand(logsCmd)
}
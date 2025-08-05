package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Open shell on selected pod (shortcut for 'pod shell')",
	Long:  "Connect to a shell on a selected application pod. This is a shortcut for 'gcpeasy pod shell'.",
	Run: func(cmd *cobra.Command, args []string) {
		if err := runPodShell(); err != nil {
			fmt.Printf("Error accessing shell: %v\n", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(shellCmd)
}
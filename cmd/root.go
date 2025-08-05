package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var version = "dev" // Will be set by build flags

var rootCmd = &cobra.Command{
	Use:     "gcpeasy",
	Version: version,
	Short:   "A CLI tool to make GCP and Kubernetes workflows easy",
	Long: `gcpeasy streamlines working with Google Cloud Platform and Kubernetes infrastructure 
by providing simple commands for common development workflows. It eliminates the need 
to remember complex kubectl and gcloud commands and automates environment switching.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
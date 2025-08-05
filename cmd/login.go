package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with Google Cloud",
	Long: `Authenticate with Google Cloud using gcloud auth login.
This command will open a browser window for authentication.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runLogin(); err != nil {
			fmt.Fprintf(os.Stderr, "Error during login: %v\n", err)
			os.Exit(1)
		}
	},
}

func runLogin() error {
	fmt.Println("🔐 Authenticating with Google Cloud...")
	
	// Check if gcloud is installed
	if _, err := exec.LookPath("gcloud"); err != nil {
		return fmt.Errorf("gcloud CLI not found. Please install the Google Cloud SDK: https://cloud.google.com/sdk/docs/install")
	}

	// Run gcloud auth login
	loginCmd := exec.Command("gcloud", "auth", "login")
	loginCmd.Stdout = os.Stdout
	loginCmd.Stderr = os.Stderr
	loginCmd.Stdin = os.Stdin

	if err := loginCmd.Run(); err != nil {
		return fmt.Errorf("gcloud auth login failed: %w", err)
	}

	fmt.Println("✅ Successfully authenticated with Google Cloud")

	// Also authenticate for kubectl
	fmt.Println("🔧 Setting up application-default credentials...")
	adcCmd := exec.Command("gcloud", "auth", "application-default", "login")
	adcCmd.Stdout = os.Stdout
	adcCmd.Stderr = os.Stderr
	adcCmd.Stdin = os.Stdin

	if err := adcCmd.Run(); err != nil {
		fmt.Println("⚠️  Warning: Failed to set up application-default credentials")
		return nil // Don't fail the whole login process
	}

	fmt.Println("✅ Authentication complete!")
	return nil
}
package cmd

import (
	"fmt"
	"gcpeasy/internal"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Open shell on selected pod",
	Long:  "Connect to a shell on a selected application pod in the current GCP environment. Tries bash, zsh, sh in order of preference.",
	Run: func(cmd *cobra.Command, args []string) {
		if err := runShell(); err != nil {
			fmt.Printf("Error accessing shell: %v\n", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(shellCmd)
}

func runShell() error {
	// Check if user is authenticated
	fmt.Println("🔍 Checking authentication...")
	if !isAuthenticated() {
		fmt.Println("❌ Not authenticated with Google Cloud")
		fmt.Println("Please run 'gcpeasy login' first to authenticate.")
		return nil
	}
	fmt.Println("✅ Authenticated")

	// Get current project
	fmt.Println("🔍 Getting current project...")
	currentProject := getCurrentProject()
	if currentProject == "" {
		fmt.Println("❌ No GCP project selected")
		fmt.Println("Please run 'gcpeasy env select' to choose an environment.")
		return nil
	}
	fmt.Printf("✅ Current project: %s\n", currentProject)

	fmt.Printf("🔍 Looking for application pods in project: %s\n", currentProject)

	selectedPod, err := internal.SetupClusterAndSelectPod(currentProject)
	if err != nil {
		if strings.Contains(err.Error(), "cancelled by user") {
			fmt.Println("Cancelled.")
			return nil
		}
		return err
	}

	fmt.Printf("🚀 Opening shell in pod: %s\n", selectedPod)
	return connectToShell(selectedPod)
}

func connectToShell(podNameWithNamespace string) error {
	parts := strings.Split(podNameWithNamespace, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid pod format: %s", podNameWithNamespace)
	}
	
	namespace := parts[0]
	podName := parts[1]
	
	fmt.Println("🎯 Connecting to shell...")
	fmt.Println("(Type 'exit' or press Ctrl+D to disconnect)")
	fmt.Println()
	
	// Try shells in order of preference: bash, zsh, sh
	shells := []string{"/bin/bash", "/bin/zsh", "/bin/sh"}
	
	for _, shell := range shells {
		fmt.Printf("Trying: %s\n", shell)
		
		cmd := exec.Command("kubectl", "exec", "-it", podName, "-n", namespace, "--", shell)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		
		err := cmd.Run()
		if err == nil {
			return nil
		}
		
		fmt.Printf("Shell %s not available, trying next option...\n", shell)
	}
	
	return fmt.Errorf("no suitable shell found in pod")
}
package cmd

import (
	"fmt"
	"gcpeasy/internal"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var railsCmd = &cobra.Command{
	Use:   "rails",
	Short: "Rails application management commands",
	Long:  "Commands for managing Rails applications running in GCP/Kubernetes environments.",
}

var railsConsoleCmd = &cobra.Command{
	Use:     "console",
	Aliases: []string{"c"},
	Short:   "Access Rails console",
	Long:    "Connect to a Rails application console running in the current GCP environment. Automatically detects Rails pods and provides console access.",
	Run: func(cmd *cobra.Command, args []string) {
		if err := runRailsConsole(); err != nil {
			fmt.Printf("Error accessing Rails console: %v\n", err)
		}
	},
}

func init() {
	railsCmd.AddCommand(railsConsoleCmd)
	rootCmd.AddCommand(railsCmd)
}

func runRailsConsole() error {
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

	fmt.Printf("🔍 Looking for Rails applications in project: %s\n", currentProject)

	// Get and select GKE cluster
	fmt.Println("🔍 Getting GKE clusters...")
	clusters, err := internal.GetGKEClusters(currentProject)
	if err != nil {
		return fmt.Errorf("failed to get GKE clusters: %w", err)
	}

	if len(clusters) == 0 {
		fmt.Println("❌ No GKE clusters found in the current project")
		fmt.Println("Make sure you have GKE clusters set up and configured.")
		return nil
	}

	selectedCluster, err := internal.SelectCluster(clusters)
	if err != nil {
		if strings.Contains(err.Error(), "cancelled by user") {
			fmt.Println("Cancelled.")
			return nil
		}
		return fmt.Errorf("failed to select cluster: %w", err)
	}
	
	fmt.Printf("🔧 Using cluster: %s in %s\n", selectedCluster.Name, selectedCluster.Location)

	// Configure kubectl for the cluster
	fmt.Println("🔧 Configuring kubectl...")
	if err := internal.ConfigureKubectl(currentProject, *selectedCluster); err != nil {
		return fmt.Errorf("failed to configure kubectl: %w", err)
	}
	fmt.Println("✅ kubectl configured")

	// Find and select pods
	fmt.Println("🔍 Searching for application pods...")
	pods, err := internal.FindApplicationPods()
	if err != nil {
		return fmt.Errorf("failed to find application pods: %w", err)
	}

	if len(pods) == 0 {
		fmt.Println("❌ No pods found")
		fmt.Println("Make sure your application is deployed and running.")
		return nil
	}

	selectedPod, err := internal.SelectPod(pods)
	if err != nil {
		if strings.Contains(err.Error(), "cancelled by user") {
			fmt.Println("Cancelled.")
			return nil
		}
		return fmt.Errorf("failed to select pod: %w", err)
	}

	fmt.Printf("🚀 Connecting to Rails console in pod: %s\n", selectedPod)
	return connectToRailsConsole(selectedPod)
}

func connectToRailsConsole(podNameWithNamespace string) error {
	parts := strings.Split(podNameWithNamespace, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid pod format: %s", podNameWithNamespace)
	}
	
	namespace := parts[0]
	podName := parts[1]
	
	fmt.Println("🎯 Connecting to Rails console...")
	fmt.Println("(Type 'exit' or press Ctrl+D to disconnect)")
	fmt.Println()
	
	// Try common Rails console commands
	consoleCommands := []string{
		"bundle exec rails console",
		"bundle exec rails c",
		"rails console",
		"rails c",
		"bin/rails console",
		"bin/rails c",
	}
	
	for _, consoleCmd := range consoleCommands {
		fmt.Printf("Trying: %s\n", consoleCmd)
		
		cmd := exec.Command("kubectl", "exec", "-it", podName, "-n", namespace, "--", "sh", "-c", consoleCmd)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		
		err := cmd.Run()
		if err == nil {
			return nil
		}
		
		fmt.Printf("Command failed, trying next option...\n")
	}
	
	// If Rails console commands fail, try a shell
	fmt.Println("Rails console commands failed, opening shell instead...")
	cmd := exec.Command("kubectl", "exec", "-it", podName, "-n", namespace, "--", "/bin/bash")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	
	return cmd.Run()
}
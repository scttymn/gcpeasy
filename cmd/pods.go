package cmd

import (
	"fmt"
	"gcpeasy/internal"
	"strings"

	"github.com/spf13/cobra"
)

var podsCmd = &cobra.Command{
	Use:   "pods",
	Short: "List application pods with status",
	Long:  "List all application pods in the current cluster with detailed status information.",
	Run: func(cmd *cobra.Command, args []string) {
		if err := listPodsWithStatus(); err != nil {
			fmt.Printf("Error listing pods: %v\n", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(podsCmd)
}

func listPodsWithStatus() error {
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

	// Setup cluster if kubectl is not configured
	if err := internal.SetupClusterIfNeeded(currentProject); err != nil {
		if strings.Contains(err.Error(), "cancelled by user") {
			fmt.Println("Cancelled.")
			return nil
		}
		return fmt.Errorf("failed to setup cluster: %w", err)
	}

	// Get detailed pod information
	fmt.Println("🔍 Gathering pod information...")
	fmt.Println()

	pods, err := internal.GetDetailedPodInfo()
	if err != nil {
		return fmt.Errorf("failed to get pod information: %w", err)
	}

	if len(pods) == 0 {
		fmt.Println("❌ No application pods found")
		fmt.Println("Make sure your applications are deployed and running.")
		return nil
	}

	// Display pods in a nice table format
	fmt.Printf("📋 Found %d application pod(s):\n", len(pods))
	fmt.Println()
	
	// Print header
	fmt.Printf("%-15s %-35s %-12s %-8s %-8s %-10s %-20s\n", 
		"NAMESPACE", "NAME", "STATUS", "READY", "RESTARTS", "AGE", "NODE")
	fmt.Println(strings.Repeat("-", 110))
	
	// Print pod info
	for _, pod := range pods {
		fmt.Printf("%-15s %-35s %-12s %-8s %-8s %-10s %-20s\n",
			truncate(pod.Namespace, 15),
			truncate(pod.Name, 35),
			pod.Status,
			pod.Ready,
			pod.Restarts,
			pod.Age,
			truncate(pod.Node, 20))
	}

	fmt.Println()
	fmt.Println("💡 Use 'gcpeasy rails console', 'gcpeasy rails logs', or 'gcpeasy shell' to interact with these pods")

	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
package cmd

import (
	"fmt"
	"gcpeasy/internal"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var clusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "Kubernetes cluster management commands",
	Long:  "Commands for managing and switching between GKE clusters in the current GCP project.",
}

var clusterListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available clusters",
	Long:  "List all available GKE clusters in the current GCP project.",
	Run: func(cmd *cobra.Command, args []string) {
		if err := listClusters(); err != nil {
			fmt.Printf("Error listing clusters: %v\n", err)
		}
	},
}

var clusterSelectCmd = &cobra.Command{
	Use:   "select [cluster-name|number]",
	Short: "Switch to a different cluster",
	Long:  "Switch to a different GKE cluster. You can specify by cluster name or the number from 'cluster list'. If no argument is provided, shows an interactive selection.",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			if err := selectClusterInteractive(); err != nil {
				fmt.Printf("Error selecting cluster: %v\n", err)
			}
		} else {
			if err := selectClusterByIdentifier(args[0]); err != nil {
				fmt.Printf("Error selecting cluster: %v\n", err)
			}
		}
	},
}

func init() {
	clusterCmd.AddCommand(clusterListCmd)
	clusterCmd.AddCommand(clusterSelectCmd)
	rootCmd.AddCommand(clusterCmd)
}

func listClusters() error {
	// Check if user is authenticated
	if !isAuthenticated() {
		fmt.Println("❌ Not authenticated with Google Cloud")
		fmt.Println("Please run 'gcpeasy login' first to authenticate.")
		return nil
	}

	// Get current project
	currentProject := getCurrentProject()
	if currentProject == "" {
		fmt.Println("❌ No GCP project selected")
		fmt.Println("Please run 'gcpeasy env select' to choose an environment.")
		return nil
	}

	fmt.Printf("Discovering GKE clusters in project: %s\n", currentProject)
	fmt.Println()

	clusters, err := internal.GetGKEClusters(currentProject)
	if err != nil {
		return fmt.Errorf("failed to discover clusters: %w", err)
	}

	if len(clusters) == 0 {
		fmt.Println("No GKE clusters found.")
		return nil
	}

	// Get current kubectl context to mark active cluster
	currentCluster := getCurrentKubectlCluster()

	fmt.Println("Available clusters:")
	fmt.Println()

	for i, cluster := range clusters {
		checkbox := "- [ ]"
		if isCurrentCluster(cluster, currentCluster) {
			checkbox = "- [x]"
		}

		fmt.Printf("%s %d. %s (%s)\n",
			checkbox,
			i+1,
			cluster.Name,
			cluster.Location,
		)
	}

	fmt.Println()
	fmt.Println("💡 Use 'gcpeasy cluster select' to switch clusters")

	return nil
}

func selectClusterInteractive() error {
	// Check if user is authenticated
	if !isAuthenticated() {
		fmt.Println("❌ Not authenticated with Google Cloud")
		fmt.Println("Please run 'gcpeasy login' first to authenticate.")
		return nil
	}

	// Get current project
	currentProject := getCurrentProject()
	if currentProject == "" {
		fmt.Println("❌ No GCP project selected")
		fmt.Println("Please run 'gcpeasy env select' to choose an environment.")
		return nil
	}

	clusters, err := internal.GetGKEClusters(currentProject)
	if err != nil {
		return fmt.Errorf("failed to get clusters: %w", err)
	}

	if len(clusters) == 0 {
		fmt.Println("No GKE clusters found.")
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

	return switchToCluster(currentProject, *selectedCluster)
}

func selectClusterByIdentifier(identifier string) error {
	// Check if user is authenticated
	if !isAuthenticated() {
		fmt.Println("❌ Not authenticated with Google Cloud")
		fmt.Println("Please run 'gcpeasy login' first to authenticate.")
		return nil
	}

	// Get current project
	currentProject := getCurrentProject()
	if currentProject == "" {
		fmt.Println("❌ No GCP project selected")
		fmt.Println("Please run 'gcpeasy env select' to choose an environment.")
		return nil
	}

	clusters, err := internal.GetGKEClusters(currentProject)
	if err != nil {
		return fmt.Errorf("failed to get clusters: %w", err)
	}

	if len(clusters) == 0 {
		fmt.Println("No GKE clusters found.")
		return nil
	}

	var selectedCluster *internal.ClusterInfo

	// Try to parse as number first
	if num, err := strconv.Atoi(identifier); err == nil {
		if num >= 1 && num <= len(clusters) {
			selectedCluster = &clusters[num-1]
		}
	}

	// If not found by number, try by cluster name
	if selectedCluster == nil {
		for _, cluster := range clusters {
			if cluster.Name == identifier {
				selectedCluster = &cluster
				break
			}
		}
	}

	if selectedCluster == nil {
		fmt.Printf("Cluster '%s' not found.\n", identifier)
		fmt.Println("Use 'gcpeasy cluster list' to see available clusters.")
		return nil
	}

	return switchToCluster(currentProject, *selectedCluster)
}

func switchToCluster(projectID string, cluster internal.ClusterInfo) error {
	fmt.Printf("Switching to cluster: %s in %s\n", cluster.Name, cluster.Location)

	if err := internal.ConfigureKubectl(projectID, cluster); err != nil {
		return fmt.Errorf("failed to switch cluster: %w", err)
	}

	fmt.Printf("✅ Successfully switched to cluster: %s\n", cluster.Name)
	return nil
}

func getCurrentKubectlCluster() string {
	// Get current kubectl context
	cmd := exec.Command("kubectl", "config", "current-context")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func isCurrentCluster(cluster internal.ClusterInfo, currentContext string) bool {
	// kubectl context format is typically gke_PROJECT_ZONE_CLUSTER-NAME
	// We'll check if the context contains the cluster name
	return strings.Contains(currentContext, cluster.Name)
}
package internal

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type ClusterInfo struct {
	Name     string
	Location string
}

// GetGKEClusters returns all GKE clusters in the specified project
func GetGKEClusters(projectID string) ([]ClusterInfo, error) {
	cmd := exec.Command("gcloud", "container", "clusters", "list", "--project", projectID, "--format=value(name,location)")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	clusterList := strings.TrimSpace(string(output))
	if clusterList == "" {
		return []ClusterInfo{}, nil
	}

	lines := strings.Split(clusterList, "\n")
	var clusters []ClusterInfo
	
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			clusters = append(clusters, ClusterInfo{
				Name:     parts[0],
				Location: parts[1],
			})
		}
	}

	return clusters, nil
}

// SelectCluster prompts user to select a cluster if multiple exist, or returns the single cluster
func SelectCluster(clusters []ClusterInfo) (*ClusterInfo, error) {
	if len(clusters) == 0 {
		return nil, fmt.Errorf("no clusters available")
	}

	if len(clusters) == 1 {
		cluster := clusters[0]
		fmt.Printf("✅ Found 1 cluster: %s in %s\n", cluster.Name, cluster.Location)
		return &cluster, nil
	}

	fmt.Printf("✅ Found %d clusters:\n", len(clusters))
	fmt.Println()
	
	for i, cluster := range clusters {
		fmt.Printf("%d. %s (%s)\n", i+1, cluster.Name, cluster.Location)
	}
	
	fmt.Println()
	fmt.Print("Select cluster (number, or 'q' to quit): ")
	
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return nil, fmt.Errorf("failed to read input")
	}
	
	input := strings.TrimSpace(scanner.Text())
	
	// Check for quit command
	if input == "q" {
		return nil, fmt.Errorf("cancelled by user")
	}
	
	num, err := strconv.Atoi(input)
	if err != nil || num < 1 || num > len(clusters) {
		return nil, fmt.Errorf("invalid selection: %s", input)
	}
	
	selectedCluster := clusters[num-1]
	return &selectedCluster, nil
}

// ConfigureKubectl configures kubectl for the specified cluster
func ConfigureKubectl(projectID string, cluster ClusterInfo) error {
	fmt.Printf("🔧 Getting credentials for cluster %s in %s...\n", cluster.Name, cluster.Location)
	cmd := exec.Command("gcloud", "container", "clusters", "get-credentials", cluster.Name, "--location", cluster.Location, "--project", projectID)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to get cluster credentials: %w", err)
	}
	
	return nil
}

// IsKubectlConfigured checks if kubectl is configured and can connect to a cluster
func IsKubectlConfigured() bool {
	cmd := exec.Command("kubectl", "cluster-info")
	err := cmd.Run()
	return err == nil
}

// GetCurrentCluster returns the current kubectl context cluster info
func GetCurrentCluster() (string, error) {
	cmd := exec.Command("kubectl", "config", "current-context")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// SetupClusterIfNeeded handles cluster setup only if kubectl is not configured
func SetupClusterIfNeeded(projectID string) error {
	// If kubectl is already configured and working, use current context
	if IsKubectlConfigured() {
		context, err := GetCurrentCluster()
		if err == nil && context != "" {
			fmt.Printf("✅ Using current cluster context: %s\n", context)
			return nil
		}
	}
	
	// kubectl not configured, need to set up cluster
	fmt.Println("🔧 kubectl not configured, setting up cluster...")
	
	clusters, err := GetGKEClusters(projectID)
	if err != nil {
		return fmt.Errorf("failed to get GKE clusters: %w", err)
	}

	if len(clusters) == 0 {
		return fmt.Errorf("no GKE clusters found in project %s", projectID)
	}

	selectedCluster, err := SelectCluster(clusters)
	if err != nil {
		return err
	}
	
	fmt.Printf("🔧 Using cluster: %s in %s\n", selectedCluster.Name, selectedCluster.Location)

	// Configure kubectl for the cluster
	fmt.Println("🔧 Configuring kubectl...")
	if err := ConfigureKubectl(projectID, *selectedCluster); err != nil {
		return fmt.Errorf("failed to configure kubectl: %w", err)
	}
	fmt.Println("✅ kubectl configured")
	
	return nil
}

// SetupClusterAndSelectPod handles cluster setup (if needed) and pod selection
func SetupClusterAndSelectPod(projectID string) (string, error) {
	// Setup cluster if kubectl is not configured
	if err := SetupClusterIfNeeded(projectID); err != nil {
		return "", err
	}

	// Find and select pods
	fmt.Println("🔍 Searching for application pods...")
	pods, err := FindApplicationPods()
	if err != nil {
		return "", fmt.Errorf("failed to find application pods: %w", err)
	}

	if len(pods) == 0 {
		fmt.Println("❌ No pods found")
		fmt.Println("Make sure your application is deployed and running.")
		return "", fmt.Errorf("no pods found")
	}

	selectedPod, err := SelectPod(pods)
	if err != nil {
		return "", err // Error already includes "cancelled by user" check
	}

	return selectedPod, nil
}
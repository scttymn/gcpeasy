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

// FindApplicationPods returns all running pods from non-system namespaces
func FindApplicationPods() ([]string, error) {
	cmd := exec.Command("kubectl", "get", "pods", "--all-namespaces", "-o", "custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,STATUS:.status.phase", "--no-headers")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var appPods []string
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	
	for _, line := range lines {
		if line == "" {
			continue
		}
		
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		
		namespace := fields[0]
		podName := fields[1]
		status := fields[2]
		
		// Skip system namespaces and non-running pods
		if isSystemNamespace(namespace) || status != "Running" {
			continue
		}
		
		appPods = append(appPods, fmt.Sprintf("%s/%s", namespace, podName))
	}

	return appPods, nil
}

// SelectPod prompts user to select a pod from the list
func SelectPod(pods []string) (string, error) {
	if len(pods) == 0 {
		return "", fmt.Errorf("no pods available")
	}

	fmt.Printf("📋 Found %d pod(s):\n", len(pods))
	fmt.Println()
	
	for i, pod := range pods {
		fmt.Printf("%d. %s\n", i+1, pod)
	}
	
	fmt.Println()
	fmt.Print("Select pod (number, or 'q' to quit): ")
	
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return "", fmt.Errorf("failed to read input")
	}
	
	input := strings.TrimSpace(scanner.Text())
	
	// Check for quit command
	if input == "q" {
		return "", fmt.Errorf("cancelled by user")
	}
	
	num, err := strconv.Atoi(input)
	if err != nil || num < 1 || num > len(pods) {
		return "", fmt.Errorf("invalid selection: %s", input)
	}
	
	return pods[num-1], nil
}

func isSystemNamespace(namespace string) bool {
	systemNamespaces := []string{"kube-system", "kube-public", "kube-node-lease", "gke-system"}
	for _, sysNs := range systemNamespaces {
		if namespace == sysNs {
			return true
		}
	}
	return false
}
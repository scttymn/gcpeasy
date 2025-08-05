package internal

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// PodInfo contains detailed information about a pod
type PodInfo struct {
	Namespace string
	Name      string
	Status    string
	Ready     string
	Restarts  string
	Age       string
	Node      string
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

// GetDetailedPodInfo returns detailed information about application pods
func GetDetailedPodInfo() ([]PodInfo, error) {
	// Use standard kubectl get pods which handles multi-container formatting better
	cmd := exec.Command("kubectl", "get", "pods", "--all-namespaces", "--no-headers")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var pods []PodInfo
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	
	for _, line := range lines {
		if line == "" {
			continue
		}
		
		// Parse standard kubectl output: NAMESPACE NAME READY STATUS RESTARTS AGE
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		
		namespace := fields[0]
		podName := fields[1]
		ready := fields[2]      // Already formatted as "1/1", "2/2", etc.
		status := fields[3]
		restarts := fields[4]   // Already summed by kubectl
		age := fields[5]
		
		// Get node info separately if needed
		node := getNodeForPod(namespace, podName)
		
		// Skip system namespaces
		if isSystemNamespace(namespace) {
			continue
		}
		
		// Include running pods and pods with issues (for debugging)
		if status == "Running" || status == "Pending" || status == "CrashLoopBackOff" || status == "Error" {
			pods = append(pods, PodInfo{
				Namespace: namespace,
				Name:      podName,
				Status:    status,
				Ready:     ready,
				Restarts:  restarts,
				Age:       age,
				Node:      node,
			})
		}
	}

	return pods, nil
}

// getNodeForPod gets the node name for a specific pod
func getNodeForPod(namespace, podName string) string {
	cmd := exec.Command("kubectl", "get", "pod", podName, "-n", namespace, "-o", "jsonpath={.spec.nodeName}")
	output, err := cmd.Output()
	if err != nil {
		return "<unknown>"
	}
	return strings.TrimSpace(string(output))
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
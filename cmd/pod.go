package cmd

import (
	"fmt"
	"gcpeasy/internal"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

var podCmd = &cobra.Command{
	Use:   "pod",
	Short: "Pod management commands",
	Long:  "Commands for managing and interacting with pods in GCP/Kubernetes environments.",
}

var podListCmd = &cobra.Command{
	Use:   "list",
	Short: "List application pods",
	Long:  "List all application pods in the current cluster. Use --status for detailed status information.",
	Run: func(cmd *cobra.Command, args []string) {
		showStatus, _ := cmd.Flags().GetBool("status")
		if err := listPods(showStatus); err != nil {
			fmt.Printf("Error listing pods: %v\n", err)
		}
	},
}

var podLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View pod logs",
	Long:  "View logs from application pods. Use -f to follow logs in real-time. Use -e/--error or -w/--warn to filter by log level.",
	Run: func(cmd *cobra.Command, args []string) {
		follow, _ := cmd.Flags().GetBool("follow")
		errorOnly, _ := cmd.Flags().GetBool("error")
		warnOnly, _ := cmd.Flags().GetBool("warn")
		infoOnly, _ := cmd.Flags().GetBool("info")
		debugOnly, _ := cmd.Flags().GetBool("debug")
		allPods, _ := cmd.Flags().GetBool("all")

		var level string
		if errorOnly {
			level = "error"
		} else if warnOnly {
			level = "warn"
		} else if infoOnly {
			level = "info"
		} else if debugOnly {
			level = "debug"
		}

		if err := runPodLogs(follow, level, allPods); err != nil {
			fmt.Printf("Error viewing logs: %v\n", err)
		}
	},
}

var podShellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Open shell on selected pod",
	Long:  "Connect to a shell on a selected application pod in the current GCP environment. Tries bash, zsh, sh in order of preference.",
	Run: func(cmd *cobra.Command, args []string) {
		if err := runPodShell(); err != nil {
			fmt.Printf("Error accessing shell: %v\n", err)
		}
	},
}

func init() {
	podListCmd.Flags().BoolP("status", "s", false, "Show detailed status information")
	podLogsCmd.Flags().BoolP("follow", "f", false, "Follow logs in real-time")
	podLogsCmd.Flags().BoolP("error", "e", false, "Show only error logs")
	podLogsCmd.Flags().BoolP("warn", "w", false, "Show only warning logs")
	podLogsCmd.Flags().BoolP("info", "i", false, "Show only info logs")
	podLogsCmd.Flags().BoolP("debug", "d", false, "Show only debug logs")
	podLogsCmd.Flags().BoolP("all", "a", false, "View logs for all application pods")

	podCmd.AddCommand(podListCmd)
	podCmd.AddCommand(podLogsCmd)
	podCmd.AddCommand(podShellCmd)
	rootCmd.AddCommand(podCmd)
}

func listPods(showStatus bool) error {
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

	fmt.Printf("📋 Found %d application pod(s):\n", len(pods))
	fmt.Println()

	if showStatus {
		// Print detailed status table
		fmt.Printf("%-15s %-35s %-12s %-8s %-8s %-10s %-20s\n",
			"NAMESPACE", "NAME", "STATUS", "READY", "RESTARTS", "AGE", "NODE")
		fmt.Println(strings.Repeat("-", 110))

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
	} else {
		// Print simple list
		fmt.Printf("%-15s %-35s\n", "NAMESPACE", "NAME")
		fmt.Println(strings.Repeat("-", 52))

		for _, pod := range pods {
			fmt.Printf("%-15s %-35s\n",
				truncate(pod.Namespace, 15),
				truncate(pod.Name, 35))
		}
	}

	fmt.Println()
	fmt.Println("💡 Use 'gcpeasy pod logs', 'gcpeasy pod shell', or 'gcpeasy rails console' to interact with these pods")

	return nil
}

func runPodLogs(follow bool, level string, allPods bool) error {
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

	if allPods {
		// Setup cluster if kubectl is not configured
		if err := internal.SetupClusterIfNeeded(currentProject); err != nil {
			if strings.Contains(err.Error(), "cancelled by user") {
				fmt.Println("Cancelled.")
				return nil
			}
			return fmt.Errorf("failed to setup cluster: %w", err)
		}

		fmt.Println("🔍 Gathering pod list...")
		pods, err := internal.FindApplicationPods()
		if err != nil {
			return fmt.Errorf("failed to find application pods: %w", err)
		}

		if len(pods) == 0 {
			fmt.Println("❌ No application pods found")
			fmt.Println("Make sure your applications are deployed and running.")
			return nil
		}

		fmt.Printf("📋 Viewing logs for %d pod(s):\n", len(pods))
		for _, p := range pods {
			fmt.Printf(" - %s\n", p)
		}
		fmt.Println()

		return viewMultiplePodLogs(pods, follow, level)
	}

	selectedPod, err := internal.SetupClusterAndSelectPod(currentProject)
	if err != nil {
		if strings.Contains(err.Error(), "cancelled by user") {
			fmt.Println("Cancelled.")
			return nil
		}
		return err
	}

	fmt.Printf("📋 Viewing logs for pod: %s\n", selectedPod)
	return viewPodLogs(selectedPod, follow, level)
}

func viewMultiplePodLogs(pods []string, follow bool, level string) error {
	if len(pods) == 0 {
		return fmt.Errorf("no pods provided")
	}

	if level != "" {
		fmt.Printf("📋 Filtering logs by level: %s\n", strings.ToUpper(level))
	}

	if follow {
		fmt.Println("🔄 Following logs from multiple pods (press Ctrl+C to stop)...")
	} else {
		fmt.Println("📋 Fetching logs from multiple pods...")
	}
	fmt.Println()

	var wg sync.WaitGroup
	errCh := make(chan error, len(pods))

	for _, pod := range pods {
		p := pod
		wg.Add(1)

		go func() {
			defer wg.Done()
			if err := viewPodLogs(p, follow, level); err != nil {
				errCh <- fmt.Errorf("%s: %w", p, err)
			}
		}()
	}

	go func() {
		wg.Wait()
		close(errCh)
	}()

	var firstErr error
	for err := range errCh {
		if firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func runPodShell() error {
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

func viewPodLogs(podNameWithNamespace string, follow bool, level string) error {
	parts := strings.Split(podNameWithNamespace, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid pod format: %s", podNameWithNamespace)
	}

	namespace := parts[0]
	podName := parts[1]

	if level != "" {
		fmt.Printf("📋 Filtering logs by level: %s\n", strings.ToUpper(level))
	}

	if follow {
		fmt.Println("🔄 Following logs (press Ctrl+C to stop)...")
	} else {
		fmt.Println("📋 Fetching logs...")
	}
	fmt.Println()

	// Build kubectl logs command
	args := []string{"logs", podName, "-n", namespace}
	if follow {
		args = append(args, "-f")
	}

	cmd := exec.Command("kubectl", args...)

	// If filtering by level, pipe through grep
	if level != "" {
		grepPatterns := getLogLevelPatterns(level)
		if len(grepPatterns) > 0 {
			// Use grep to filter logs
			grepArgs := []string{"-E", "-i", strings.Join(grepPatterns, "|")}

			kubectlCmd := exec.Command("kubectl", args...)
			grepCmd := exec.Command("grep", grepArgs...)

			// Pipe kubectl output to grep
			grepCmd.Stdin, _ = kubectlCmd.StdoutPipe()
			grepCmd.Stdout = os.Stdout
			grepCmd.Stderr = os.Stderr

			kubectlCmd.Stderr = os.Stderr

			if err := kubectlCmd.Start(); err != nil {
				return err
			}
			if err := grepCmd.Start(); err != nil {
				return err
			}

			if err := kubectlCmd.Wait(); err != nil {
				return err
			}
			return grepCmd.Wait()
		}
	}

	// No filtering, run kubectl directly
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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

func getLogLevelPatterns(level string) []string {
	switch strings.ToLower(level) {
	case "error", "err":
		return []string{"ERROR", "FATAL", "Exception", "Error"}
	case "warn", "warning":
		return []string{"WARN", "WARNING"}
	case "info":
		return []string{"INFO"}
	case "debug":
		return []string{"DEBUG"}
	default:
		return []string{}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

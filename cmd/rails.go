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

var railsLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View Rails application logs",
	Long:  "View logs from Rails application pods. Use -f to follow logs in real-time. Use -e/--error or -w/--warn to filter by log level.",
	Run: func(cmd *cobra.Command, args []string) {
		follow, _ := cmd.Flags().GetBool("follow")
		errorOnly, _ := cmd.Flags().GetBool("error")
		warnOnly, _ := cmd.Flags().GetBool("warn")
		infoOnly, _ := cmd.Flags().GetBool("info")
		debugOnly, _ := cmd.Flags().GetBool("debug")
		
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
		
		if err := runRailsLogs(follow, level); err != nil {
			fmt.Printf("Error viewing logs: %v\n", err)
		}
	},
}

func init() {
	railsLogsCmd.Flags().BoolP("follow", "f", false, "Follow logs in real-time")
	railsLogsCmd.Flags().BoolP("error", "e", false, "Show only error logs")
	railsLogsCmd.Flags().BoolP("warn", "w", false, "Show only warning logs")
	railsLogsCmd.Flags().BoolP("info", "i", false, "Show only info logs")
	railsLogsCmd.Flags().BoolP("debug", "d", false, "Show only debug logs")
	railsCmd.AddCommand(railsConsoleCmd)
	railsCmd.AddCommand(railsLogsCmd)
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

	selectedPod, err := internal.SetupClusterAndSelectPod(currentProject)
	if err != nil {
		if strings.Contains(err.Error(), "cancelled by user") {
			fmt.Println("Cancelled.")
			return nil
		}
		return err
	}

	fmt.Printf("🚀 Connecting to Rails console in pod: %s\n", selectedPod)
	return connectToRailsConsole(selectedPod)
}

func runRailsLogs(follow bool, level string) error {
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
package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type Environment struct {
	Name      string
	ProjectID string
}

type GCPProject struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
}

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Environment management commands",
	Long:  "Commands for managing and switching between GCP environments.",
}

var envListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available environments",
	Long:  "List all available GCP projects. Use --status to include connectivity status (slower).",
	Run: func(cmd *cobra.Command, args []string) {
		showStatus, _ := cmd.Flags().GetBool("status")
		if err := listEnvironments(showStatus); err != nil {
			fmt.Printf("Error listing environments: %v\n", err)
		}
	},
}

var envSelectCmd = &cobra.Command{
	Use:   "select [project-id|number]",
	Short: "Switch to a different environment",
	Long:  "Switch to a different GCP project environment. You can specify by project ID, project name, or the number from 'env list'. If no argument is provided, shows an interactive selection.",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			if err := selectEnvironmentInteractive(); err != nil {
				fmt.Printf("Error selecting environment: %v\n", err)
			}
		} else {
			if err := selectEnvironment(args[0]); err != nil {
				fmt.Printf("Error selecting environment: %v\n", err)
			}
		}
	},
}

func init() {
	envListCmd.Flags().Bool("status", false, "Include connectivity status (slower)")
	envCmd.AddCommand(envListCmd)
	envCmd.AddCommand(envSelectCmd)
	rootCmd.AddCommand(envCmd)
}

func listEnvironments(showStatus bool) error {
	// Check if user is authenticated
	if !isAuthenticated() {
		fmt.Println("❌ Not authenticated with Google Cloud")
		fmt.Println("Please run 'gcpeasy login' first to authenticate.")
		return nil
	}

	fmt.Println("Discovering GCP projects...")
	fmt.Println()

	projects, err := getGCPProjects()
	if err != nil {
		return fmt.Errorf("failed to discover projects: %w", err)
	}

	if len(projects) == 0 {
		fmt.Println("No GCP projects found.")
		return nil
	}

	currentProject := getCurrentProject()
	
	fmt.Println("Available environments:")
	fmt.Println()
	
	for i, project := range projects {
		checkbox := "- [ ]"
		if project.ProjectID == currentProject {
			checkbox = "- [x]"
		}
		
		if showStatus {
			status := getProjectStatus(project.ProjectID)
			fmt.Printf("%s %d. %s (%s) %s\n", 
				checkbox,
				i+1, 
				project.ProjectID,
				project.Name, 
				status,
			)
		} else {
			fmt.Printf("%s %d. %s (%s)\n", 
				checkbox,
				i+1, 
				project.ProjectID,
				project.Name,
			)
		}
	}
	
	if !showStatus {
		fmt.Println()
		fmt.Println("💡 Use 'gcpeasy env list --status' to see connectivity status")
	}
	
	return nil
}

func getGCPProjects() ([]GCPProject, error) {
	cmd := exec.Command("gcloud", "projects", "list", "--format=json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list GCP projects: %w", err)
	}

	var projects []GCPProject
	if err := json.Unmarshal(output, &projects); err != nil {
		return nil, fmt.Errorf("failed to parse projects JSON: %w", err)
	}

	return projects, nil
}

func getCurrentProject() string {
	cmd := exec.Command("gcloud", "config", "get-value", "project")
	output, _ := cmd.Output()
	return strings.TrimSpace(string(output))
}

func getProjectStatus(projectID string) string {
	// Check if we can access the project
	cmd := exec.Command("gcloud", "projects", "describe", projectID)
	if err := cmd.Run(); err != nil {
		return "✗ Not accessible"
	}
	
	// Check if there are any GKE clusters in this project
	cmd = exec.Command("gcloud", "container", "clusters", "list", "--project", projectID, "--format=value(name)")
	output, err := cmd.Output()
	if err == nil && len(strings.TrimSpace(string(output))) > 0 {
		return "✓ Connected (has clusters)"
	}
	
	return "✓ Accessible"
}

func isAuthenticated() bool {
	cmd := exec.Command("gcloud", "auth", "list", "--filter=status:ACTIVE", "--format=value(account)")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(output))) > 0
}

func selectEnvironment(identifier string) error {
	if !isAuthenticated() {
		fmt.Println("❌ Not authenticated with Google Cloud")
		fmt.Println("Please run 'gcpeasy login' first to authenticate.")
		return nil
	}

	projects, err := getGCPProjects()
	if err != nil {
		return fmt.Errorf("failed to get projects: %w", err)
	}

	if len(projects) == 0 {
		fmt.Println("No GCP projects found.")
		return nil
	}

	var selectedProject *GCPProject

	// Try to parse as number first
	if num, err := strconv.Atoi(identifier); err == nil {
		if num >= 1 && num <= len(projects) {
			selectedProject = &projects[num-1]
		}
	}

	// If not found by number, try by project ID or name
	if selectedProject == nil {
		for _, project := range projects {
			if project.ProjectID == identifier || project.Name == identifier {
				selectedProject = &project
				break
			}
		}
	}

	if selectedProject == nil {
		fmt.Printf("Environment '%s' not found.\n", identifier)
		fmt.Println("Use 'gcpeasy env list' to see available environments.")
		return nil
	}

	return switchToProject(selectedProject.ProjectID)
}

func selectEnvironmentInteractive() error {
	if !isAuthenticated() {
		fmt.Println("❌ Not authenticated with Google Cloud")
		fmt.Println("Please run 'gcpeasy login' first to authenticate.")
		return nil
	}

	projects, err := getGCPProjects()
	if err != nil {
		return fmt.Errorf("failed to get projects: %w", err)
	}

	if len(projects) == 0 {
		fmt.Println("No GCP projects found.")
		return nil
	}

	fmt.Println("Available environments:")
	fmt.Println()

	currentProject := getCurrentProject()
	for i, project := range projects {
		checkbox := "- [ ]"
		if project.ProjectID == currentProject {
			checkbox = "- [x]"
		}
		fmt.Printf("%s %d. %s (%s)\n", checkbox, i+1, project.ProjectID, project.Name)
	}

	fmt.Println()
	fmt.Print("Select environment (number): ")

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return fmt.Errorf("failed to read input")
	}

	input := strings.TrimSpace(scanner.Text())
	num, err := strconv.Atoi(input)
	if err != nil || num < 1 || num > len(projects) {
		fmt.Printf("Invalid selection: %s\n", input)
		return nil
	}

	selectedProject := projects[num-1]
	return switchToProject(selectedProject.ProjectID)
}

func switchToProject(projectID string) error {
	fmt.Printf("Switching to project: %s\n", projectID)

	cmd := exec.Command("gcloud", "config", "set", "project", projectID)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to switch project: %w", err)
	}

	fmt.Printf("✅ Successfully switched to project: %s\n", projectID)
	return nil
}
package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
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

func init() {
	envListCmd.Flags().Bool("status", false, "Include connectivity status (slower)")
	envCmd.AddCommand(envListCmd)
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
		marker := ""
		if project.ProjectID == currentProject {
			marker = " *current*"
		}
		
		if showStatus {
			status := getProjectStatus(project.ProjectID)
			fmt.Printf("%d. %s (%s) %s%s\n", 
				i+1, 
				project.ProjectID,
				project.Name, 
				status,
				marker,
			)
		} else {
			fmt.Printf("%d. %s (%s)%s\n", 
				i+1, 
				project.ProjectID,
				project.Name,
				marker,
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
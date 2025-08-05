# gcpeasy

A CLI tool to make GCP and Kubernetes workflows easy. gcpeasy streamlines working with Google Cloud Platform and Kubernetes infrastructure by providing simple commands for common development workflows. It eliminates the need to remember complex kubectl and gcloud commands and automates environment switching.

## Table of Contents

- [Features](#features)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Commands](#commands)
  - [Authentication](#authentication)
  - [Environment Management](#environment-management)
  - [Cluster Management](#cluster-management)
  - [Pod Operations](#pod-operations)
  - [Rails Support](#rails-support)
- [Usage Patterns](#usage-patterns)
  - [Interactive Selection](#interactive-selection)
  - [Direct Selection](#direct-selection)
  - [Current Context](#current-context)
- [How It Works](#how-it-works)
  - [Cluster Behavior](#cluster-behavior)
  - [Environment Behavior](#environment-behavior)
  - [Pod Selection](#pod-selection)
- [Project Structure](#project-structure)
- [Contributing](#contributing)
- [License](#license)

## Features

- 🔐 **Authentication**: Simple GCP authentication setup
- 🌍 **Environment Management**: Switch between GCP projects with ease
- ⚙️ **Cluster Management**: Manage and switch between GKE clusters
- 🐳 **Pod Operations**: Interactive pod selection and management
- 🚀 **Rails Support**: Direct Rails console and log access
- 💻 **Shell Access**: Connect to pod shells with automatic fallback
- 📋 **Status Overview**: View pod status and cluster information

## Prerequisites

- [Google Cloud SDK (gcloud)](https://cloud.google.com/sdk/docs/install)
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
- Access to GCP projects and GKE clusters
- Go 1.19+ (for building from source)

## Installation

```bash
# Clone the repository
git clone <repository-url>
cd gcpeasy

# Build the binary
go build -o gcpeasy

# (Optional) Add to PATH
mv gcpeasy /usr/local/bin/
```

## Quick Start

1. **Authenticate with Google Cloud:**
   ```bash
   gcpeasy login
   ```

2. **Select your environment (GCP project):**
   ```bash
   gcpeasy env list
   gcpeasy env select
   ```

3. **Select your cluster:**
   ```bash
   gcpeasy cluster list
   gcpeasy cluster select
   ```

4. **Start using the tools:**
   ```bash
   gcpeasy pods              # List all pods
   gcpeasy rails console     # Access Rails console
   gcpeasy shell            # Get shell access to a pod
   ```

## Commands

### Authentication
- `gcpeasy login` - Authenticate with Google Cloud

### Environment Management
- `gcpeasy env list` - List available GCP projects
  - `--status` - Include connectivity status (slower)
- `gcpeasy env select [project]` - Switch to a different project
  - Interactive selection if no project specified
  - Supports selection by project ID, name, or number

### Cluster Management
- `gcpeasy cluster list` - List available GKE clusters
- `gcpeasy cluster select [cluster]` - Switch to a different cluster
  - Interactive selection if no cluster specified
  - Supports selection by cluster name or number

### Pod Operations
- `gcpeasy pods` - List all application pods with status information
- `gcpeasy shell` - Open interactive shell on selected pod
  - Tries bash, zsh, sh in order of preference

### Rails Support
- `gcpeasy rails console` (or `gcpeasy rails c`) - Access Rails console
- `gcpeasy rails logs` - View Rails application logs
  - `-f, --follow` - Follow logs in real-time
  - `-e, --error` - Show only error logs
  - `-w, --warn` - Show only warning logs
  - `-i, --info` - Show only info logs
  - `-d, --debug` - Show only debug logs

## Usage Patterns

### Interactive Selection
Most commands support interactive selection with numbered lists:

```bash
$ gcpeasy env list
Available environments:

- [ ] 1. project-dev (Development Project)
- [x] 2. project-staging (Staging Project)  
- [ ] 3. project-prod (Production Project)

$ gcpeasy cluster select
✅ Found 2 clusters:

1. dev-cluster (us-central1)
2. prod-cluster (us-east1)

Select cluster (number, or 'q' to quit): 1
```

### Direct Selection
Commands also support direct selection by name or number:

```bash
gcpeasy env select 2                    # Select by number
gcpeasy env select project-prod         # Select by project ID
gcpeasy cluster select prod-cluster     # Select by cluster name
```

### Current Context
gcpeasy respects and manages your current context:

- **Project context**: Set with `gcpeasy env select`, used by all commands
- **Cluster context**: Set with `gcpeasy cluster select`, configures kubectl
- **Smart defaults**: Auto-selects when only one option available

## How It Works

### Cluster Behavior
- If kubectl is configured and working → uses current context
- If kubectl not configured → prompts for cluster selection and configures kubectl
- Use `gcpeasy cluster select` to explicitly change clusters

### Environment Behavior  
- If only 1 project accessible → auto-selects
- If multiple projects → prompts for selection
- Use `gcpeasy env select` to change projects

### Pod Selection
- Shows only application pods (filters out system namespaces)
- Displays running pods and pods with issues for debugging
- Consistent numbered selection across all pod-related commands

## Project Structure

```
gcpeasy/
├── cmd/                    # CLI commands
│   ├── root.go            # Root command and global flags
│   ├── login.go           # Authentication command
│   ├── env.go             # Environment/project management
│   ├── cluster.go         # Cluster management
│   ├── pods.go            # Pod listing command
│   ├── shell.go           # Shell access command
│   └── rails.go           # Rails-specific commands
├── internal/              # Internal packages
│   ├── kubernetes.go      # Kubernetes cluster operations
│   └── pod.go            # Pod operations and selection
├── main.go               # Application entry point
└── README.md            # This file
```

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details.
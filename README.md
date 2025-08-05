# Carebility CLI

A command-line tool to simplify common development tasks for the Carebility infrastructure and applications.

## Overview

The Carebility CLI streamlines working with our complex Kubernetes infrastructure by providing simple commands for common development workflows. It eliminates the need to remember complex kubectl commands and automates environment switching.

## Features

### Core Infrastructure
- [x] Setup Go project with cobra CLI framework
- [ ] Implement gcloud authentication wrapper
- [ ] Create environment discovery and selection
- [ ] Add kubectl context management

### Rails Operations
- [ ] Implement rails console command with pod auto-discovery
- [ ] Add log tailing with pod selection and filtering
- [ ] Create deployment restart functionality
- [ ] Add environment variable inspection

### User Experience
- [ ] Design intuitive command structure and help system
- [ ] Add safety confirmations for destructive operations
- [ ] Implement configuration file for user preferences

## Planned Commands

```bash
# Authentication and environment management
carebility-cli login
carebility-cli env list
carebility-cli env select demo
carebility-cli status

# Rails operations
carebility-cli console              # Connect to Rails console
carebility-cli logs -f              # Tail application logs
carebility-cli restart web-app      # Restart deployment
carebility-cli env-vars             # Show environment variables

# Information commands
carebility-cli pods                 # List relevant pods
carebility-cli deployments         # Show deployment status
```

## Environment Structure

Each Carebility environment follows these patterns:
- **GCP Projects**: `carebility-us-<environment>-<suffix>` or `us-<environment>-<suffix>`
- **Cluster Names**: `carebility-k8s-cluster-<environment>`
- **Location**: `us-central1`

### Current Environments
- **Dev**: `carebility-us-dev-8b82` / `carebility-k8s-cluster-dev`
- **Testing**: `us-testing-451620` / `carebility-k8s-cluster-testing`
- **Demo**: `carebility-us-demo-84fc` / `carebility-k8s-cluster-demo`
- **Staging**: `carebility-us-staging-bc2d` / `carebility-k8s-cluster-staging`
- **Production**: `carebility-us-prod-8e73` / `carebility-k8s-cluster-prod`

## Prerequisites

- [gcloud CLI](https://cloud.google.com/sdk/docs/install)
- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl)

## Installation

```bash
# Build from source
go build -o carebility-cli cmd/main.go

# Or install globally
go install
```

## Configuration

The CLI will store configuration in `~/.config/carebility-cli/config.yaml`:

```yaml
current_environment: demo
default_namespace: carebility
```

## Usage Examples

### Connect to Demo Environment Rails Console
```bash
# Discover and select environment
carebility-cli env select demo

# Connect to Rails console (auto-finds web pod)
carebility-cli console
```

### Tail Logs with Filtering
```bash
# Tail all web app logs
carebility-cli logs web-app -f

# Filter logs by keyword
carebility-cli logs web-app -f --grep "ERROR"
```

### Restart Deployment
```bash
# Restart with confirmation
carebility-cli restart web-app

# Force restart without confirmation
carebility-cli restart web-app --force
```

## Development

### Project Structure
```
├── cmd/                    # CLI entry points
├── internal/              # Private application code
│   ├── commands/          # Command implementations
│   ├── config/           # Configuration management
│   └── k8s/              # Kubernetes operations
├── pkg/                   # Public library code
└── README.md
```

### Adding New Commands

1. Create command file in `internal/commands/`
2. Implement the command logic
3. Register with Cobra in `cmd/root.go`
4. Add tests and documentation

## Contributing

1. Follow the existing command patterns
2. Add appropriate error handling and user feedback
3. Include safety confirmations for destructive operations
4. Update documentation and examples

## Security

- Never log or store sensitive credentials
- Always verify environment before destructive operations
- Use read-only operations when possible
- Implement proper timeout handling for long operations
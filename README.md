# gcpeasy

gcpeasy is a terminal workspace for Google Cloud and Kubernetes day-to-day work. It gives you a LazyGit-style TUI for switching GCP projects, GKE clusters, and pods, then running logs, shells, Rails consoles, and other pod tasks without repeatedly typing `gcpeasy <command>`.

The original CLI commands still work for scripting and one-off use, but the default experience is now the interactive TUI.

## Features

- Interactive TUI for environments, clusters, pods, and task output
- Browser-based Google Cloud authentication flow when needed
- Cached left-pane context so startup and refreshes stay usable
- Background refresh indicator for long-running `gcloud` and `kubectl` calls
- Pod logs, follow logs, shell access, Rails console, and describe actions from the TUI
- Persistent visibility preferences for hiding noisy environments, clusters, and pods
- CLI commands for automation and direct selection

## Prerequisites

- [Google Cloud SDK](https://cloud.google.com/sdk/docs/install), including `gcloud`
- Google Cloud Auth Plugin:
  ```bash
  gcloud components install gke-gcloud-auth-plugin
  ```
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
- Access to the GCP projects and GKE clusters you want to manage
- Go 1.25+ when building from source

## Installation

Download the latest release for your platform from [GitHub Releases](https://github.com/scttymn/gcpeasy/releases).

### Linux

```bash
curl -L https://github.com/scttymn/gcpeasy/releases/latest/download/gcpeasy-linux-amd64.tar.gz | tar xz
sudo mv gcpeasy-linux-amd64 /usr/local/bin/gcpeasy
```

For ARM64 Linux, use `gcpeasy-linux-arm64.tar.gz`.

### macOS

```bash
curl -L https://github.com/scttymn/gcpeasy/releases/latest/download/gcpeasy-macos-arm64.tar.gz | tar xz
sudo mv gcpeasy-macos-arm64 /usr/local/bin/gcpeasy
```

For Intel Macs, use `gcpeasy-macos-amd64.tar.gz`.

### Windows

Download `gcpeasy-windows-amd64.zip` from the releases page and extract `gcpeasy.exe`.

### Build From Source

```bash
git clone git@github.com:scttymn/gcpeasy.git
cd gcpeasy
mise install
mise exec -- go build -o gcpeasy .
```

If you do not use mise, install Go 1.25+ and run:

```bash
go build -o gcpeasy .
```

Verify the installed binary:

```bash
gcpeasy --version
```

## Quick Start

Open gcpeasy:

```bash
gcpeasy
```

If you are not authenticated, gcpeasy shows an authentication dialog. Choose **Authenticate** to launch the normal browser-based `gcloud auth login` flow, or choose **Quit** to exit.

Once authenticated:

1. Select an environment to switch GCP projects.
2. Select a cluster to configure kubectl credentials.
3. Select a pod to make it the target for pod actions.
4. Use the bottom key hints to run actions for the highlighted resource.

Task output appears on the right. Interactive sessions such as pod shells and Rails consoles take over the full terminal (suspending the UI) so copy/paste, scrollback, and colors work like a normal shell; gcpeasy returns when you exit the session.

## TUI Controls

| Key | Action |
| --- | --- |
| `tab`, `shift+tab` | Move focus between panes |
| `1`, `2`, `3` | Focus environments, clusters, or pods |
| `0` | Focus task output |
| `j`, `k`, arrows | Move selection or scroll output |
| `enter` | Run the primary action for the focused pane |
| `space` | Open the command palette |
| `?` | Open help |
| `r` | Refresh context |
| `q` | Quit |

When a pod is highlighted:

| Key | Action |
| --- | --- |
| `l` | View pod logs |
| `f` | Follow pod logs |
| `s` | Open a pod shell |
| `c` | Open a Rails console |
| `d` | Describe the pod |

When the task output pane is focused and an interactive task is running, typing goes to the task. Use `ctrl+g` to return focus to the side panes, `ctrl+c` to interrupt the task, and `x` to stop it.

## Command Palette

Press `space` to open the command palette. It contains global, occasional actions:

- Refresh context
- Login to Google Cloud or logout from Google Cloud
- Reset visibility

Pod-specific actions stay in the footer hints when a pod is highlighted.

## Visibility Preferences

To reduce noise in the left panes:

- Highlight an environment, cluster, or pod and press `h` to hide it.
- Press `H` to temporarily show hidden and visible items together.
- While hidden items are visible, select a hidden item and press `h` again to unhide it.
- Use **Reset visibility** in the command palette to clear all hidden items.

Hidden items are stored separately from cached context in:

```text
~/.config/gcpeasy/tui-preferences.json
```

Set `GCPEASY_CONFIG_DIR` to override this location.

## Cached Context

The TUI stores the last known left-pane state in:

```text
~/.config/gcpeasy/tui-state.json
```

This lets gcpeasy show the last known environments, clusters, and pods immediately while `gcloud` and `kubectl` refresh in the background. The cache is only for display state; hidden visibility preferences are stored separately.

## CLI Commands

The TUI is the default, but direct commands are still available.

### TUI

```bash
gcpeasy
gcpeasy tui
gcpeasy ui
```

### Authentication

```bash
gcpeasy login
gcpeasy logout
```

`login` runs `gcloud auth login` and `gcloud auth application-default login`.

### Environments

```bash
gcpeasy env list
gcpeasy env list --status
gcpeasy env select
gcpeasy env select <project-id-or-number>
```

### Clusters

```bash
gcpeasy cluster list
gcpeasy cluster select
gcpeasy cluster select <cluster-name-or-number>
```

Cluster selection runs `gcloud container clusters get-credentials` for the selected GKE cluster.

### Pods

```bash
gcpeasy pod list
gcpeasy pod list --status
gcpeasy pod logs
gcpeasy pod logs --follow
gcpeasy pod logs --all
gcpeasy pod logs --error
gcpeasy pod logs --warn
gcpeasy pod logs --info
gcpeasy pod logs --debug
gcpeasy pod shell
```

Shortcuts:

```bash
gcpeasy logs
gcpeasy shell
```

### Rails

```bash
gcpeasy rails console
gcpeasy rails c
```

`gcpeasy rails logs` still exists for compatibility, but it is deprecated. Use `gcpeasy pod logs` instead.

## How Context Works

- Project context comes from `gcloud config get-value project`.
- Environment selection updates the active gcloud project.
- Cluster selection updates kubectl credentials for the active project.
- Pods are discovered from the current kubectl context and system namespaces are filtered out.
- If a command needs a cluster and kubectl is not configured, gcpeasy prompts for cluster selection.

## Development

Run tests:

```bash
mise exec -- go test ./...
```

Build locally:

```bash
mise exec -- go build ./...
```

The release workflow runs on pushed tags matching `v*`. The tag is used as the binary version via Go linker flags.

## Project Structure

```text
gcpeasy/
├── .github/workflows/     # Release automation
├── cmd/                   # CLI commands and TUI
│   ├── root.go            # Root command and version flag
│   ├── tui.go             # Interactive terminal UI
│   ├── tui_test.go        # TUI behavior tests
│   ├── auth.go            # Login/logout commands
│   ├── env.go             # GCP project management
│   ├── cluster.go         # GKE cluster management
│   ├── pod.go             # Pod list/log/shell commands
│   ├── logs.go            # Logs shortcut
│   ├── shell.go           # Shell shortcut
│   └── rails.go           # Rails console/log commands
├── internal/              # Kubernetes helpers
├── main.go                # Application entry point
├── go.mod
└── README.md
```

package cmd

import (
	"encoding/json"
	"fmt"
	"gcpeasy/internal"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
	"github.com/spf13/cobra"
)

type tuiPanel int

const (
	panelEnvironments tuiPanel = iota
	panelClusters
	panelPods
	panelOutput
	panelCount
)

type tuiModel struct {
	focus tuiPanel

	width  int
	height int

	authenticated  bool
	currentProject string
	currentCluster string
	selectedPod    string

	projects []GCPProject
	clusters []internal.ClusterInfo
	pods     []internal.PodInfo

	cursors            map[tuiPanel]int
	hiddenEnvironments map[string]bool
	hiddenClusters     map[string]bool
	hiddenPods         map[string]bool
	showHidden         bool

	loading bool
	status  string
	err     error

	authDialog    bool
	authChoice    int
	commandModal  bool
	commandCursor int
	helpModal     bool
	cacheLoaded   bool
	cacheHasPanes bool
	cacheAge      time.Duration
	pendingCache  *tuiStateCache
	booting       bool
	refreshModal  bool

	output         []string
	outputRow      int
	outputCol      int
	outputViewport viewport.Model
	spinner        spinner.Model
	refreshOutput  bool
	task           *runningTask
}

type tuiStateMsg struct {
	authenticated  bool
	currentProject string
	currentCluster string

	projects []GCPProject
	clusters []internal.ClusterInfo
	pods     []internal.PodInfo

	projectsLoaded bool
	clustersLoaded bool
	podsLoaded     bool

	warnings []string
	err      error
}

// loadStartedMsg carries the channel that a context load streams progress and
// its final tuiStateMsg over, so the UI can report each step as it runs.
type loadStartedMsg struct {
	ch chan tea.Msg
}

// loadProgressMsg updates the status line ("Checking authentication",
// "Loading projects", …) while a context load is in flight.
type loadProgressMsg struct {
	ch     chan tea.Msg
	status string
}

type tuiStateCache struct {
	Authenticated  bool                   `json:"authenticated"`
	CurrentProject string                 `json:"currentProject"`
	CurrentCluster string                 `json:"currentCluster"`
	SelectedPod    string                 `json:"selectedPod"`
	Projects       []GCPProject           `json:"projects"`
	Clusters       []internal.ClusterInfo `json:"clusters"`
	Pods           []internal.PodInfo     `json:"pods"`
	CachedAt       time.Time              `json:"cachedAt"`
}

type tuiPreferences struct {
	HiddenEnvironments []string `json:"hiddenEnvironments,omitempty"`
	HiddenClusters     []string `json:"hiddenClusters,omitempty"`
	HiddenPods         []string `json:"hiddenPods,omitempty"`
}

type tuiOperationMsg struct {
	label   string
	output  string
	err     error
	refresh bool
}

type taskSpec struct {
	title       string
	name        string
	args        []string
	refresh     bool
	interactive bool
}

type runningTask struct {
	spec taskSpec
	cmd  *exec.Cmd
	pty  *os.File
	out  <-chan string
	done <-chan error
}

type taskStartedMsg struct {
	spec taskSpec
	task *runningTask
	err  error
}

type taskOutputMsg struct {
	text string
	ok   bool
}

type taskDoneMsg struct {
	spec taskSpec
	err  error
}

type authDoneMsg struct {
	err error
}

type tuiLoginCommand struct{}

func (tuiLoginCommand) Run() error {
	return runLogin()
}

func (tuiLoginCommand) SetStdin(io.Reader) {}

func (tuiLoginCommand) SetStdout(io.Writer) {}

func (tuiLoginCommand) SetStderr(io.Writer) {}

// tuiInteractiveCommand runs an interactive remote session (pod shell, Rails
// console) with the user's real terminal attached. It implements tea.ExecCommand
// so tea.Exec releases the TUI (exits the alt screen, restores cooked mode),
// runs the command against the actual terminal, then restores the TUI on exit.
// This is what makes copy/paste, scrollback, colors, and line editing behave
// natively — kubectl exec -it talks straight to the terminal instead of through
// a captured PTY re-rendered inside a viewport.
type tuiInteractiveCommand struct {
	script string
}

func (c tuiInteractiveCommand) Run() error {
	cmd := exec.Command("sh", "-lc", c.script)
	// Keep the local gcloud/kubectl credential resolution non-interactive so the
	// session reuses the already signed-in account. With a real terminal now
	// attached (via tea.Exec), the gcloud get-credentials refresh would otherwise
	// trigger an interactive reauthentication prompt and ask for a password; the
	// previous captured-PTY path suppressed this by running under TERM=dumb. This
	// is scoped to the local kubectl client and is not propagated into the pod,
	// so the remote shell keeps its real terminal (colors, line editing) intact.
	cmd.Env = append(os.Environ(), "CLOUDSDK_CORE_DISABLE_PROMPTS=1")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (tuiInteractiveCommand) SetStdin(io.Reader) {}

func (tuiInteractiveCommand) SetStdout(io.Writer) {}

func (tuiInteractiveCommand) SetStderr(io.Writer) {}

type tuiCommandItem struct {
	id          string
	title       string
	description string
	enabled     bool
	reason      string
}

var tuiCmd = &cobra.Command{
	Use:     "tui",
	Aliases: []string{"ui"},
	Short:   "Open the interactive terminal UI",
	Long:    "Open the interactive terminal UI for switching environments, clusters, pods, and running common commands.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI()
	},
}

var (
	tuiTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("81"))
	tuiPanelTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("229"))
	tuiMutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))
	tuiSelectedRowStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("230")).
				Background(lipgloss.Color("62"))
	tuiHiddenRowStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240"))
	tuiHiddenSelectedRowStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("244")).
					Background(lipgloss.Color("62"))
	tuiActiveBorderColor   = lipgloss.Color("149")
	tuiInactiveBorderColor = lipgloss.Color("246")
	tuiSuccessStyle        = lipgloss.NewStyle().
				Foreground(lipgloss.Color("42"))
	tuiErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("203"))
	tuiHelpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("110"))
	tuiButtonStyle = lipgloss.NewStyle().
			Padding(0, 2).
			Foreground(lipgloss.Color("252")).
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("244"))
	tuiActiveButtonStyle = lipgloss.NewStyle().
				Padding(0, 2).
				Bold(true).
				Foreground(lipgloss.Color("230")).
				Background(lipgloss.Color("62")).
				Border(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("149"))
)

func init() {
	rootCmd.AddCommand(tuiCmd)
}

func runTUI() error {
	program := tea.NewProgram(newTUIModel(), tea.WithAltScreen())
	_, err := program.Run()
	return err
}

func newTUIModel() tuiModel {
	model := tuiModel{
		focus:              panelEnvironments,
		cursors:            map[tuiPanel]int{},
		hiddenEnvironments: map[string]bool{},
		hiddenClusters:     map[string]bool{},
		hiddenPods:         map[string]bool{},
		status:             "Checking authentication",
		loading:            true,
		booting:            true,
		outputViewport:     viewport.New(20, 8),
		spinner:            spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
	if preferences, ok := loadTUIPreferences(); ok {
		model.applyPreferences(preferences)
	}
	if cache, ok := loadTUIStateCache(); ok && cacheHasUsefulData(cache) {
		model.pendingCache = &cache
	}
	model.refreshOutput = true
	model.setOutput("Refreshing context...")
	return model
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(loadTUIState(), m.spinner.Tick)
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeTaskPTY()
		m.syncOutputViewport(true)
		return m, nil
	case spinner.TickMsg:
		if !m.loading && !m.refreshModal && !m.booting {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		return m.handleKey(msg)
	case taskInputMsg:
		if m.task != nil && m.task.pty != nil {
			_, _ = m.task.pty.Write([]byte(string(msg)))
		}
		return m, nil
	case loadStartedMsg:
		return m, waitForLoadEvent(msg.ch)
	case loadProgressMsg:
		// Report the current step on the splash / status line, then keep
		// listening for the next step (or the final tuiStateMsg).
		m.status = msg.status
		return m, waitForLoadEvent(msg.ch)
	case tuiStateMsg:
		m.booting = false
		if !msg.authenticated {
			m.retainPendingCache()
		}
		m.loading = false
		m.refreshModal = false
		m.err = msg.err
		m.authenticated = msg.authenticated
		m.currentProject = msg.currentProject
		m.currentCluster = msg.currentCluster
		if msg.err == nil && msg.authenticated && m.pendingCache != nil {
			m.applyPendingCache(*m.pendingCache, msg)
			m.pendingCache = nil
		}
		if !msg.authenticated {
			m.clearCachedContext()
		}
		if msg.projectsLoaded {
			m.projects = msg.projects
		}
		if msg.clustersLoaded {
			m.clusters = msg.clusters
		}
		if msg.podsLoaded {
			m.pods = msg.pods
		}
		m.validateSelectedPod()
		m.clampCursors()

		switch {
		case msg.err != nil:
			m.status = "Unable to refresh context"
		case len(msg.warnings) > 0:
			m.status = strings.Join(msg.warnings, " | ")
		case !msg.authenticated:
			m.status = "Authentication required"
		default:
			m.status = "Ready"
		}

		m.authDialog = !msg.authenticated && m.task == nil
		if msg.err == nil && msg.authenticated {
			m.saveCache()
			m.cacheLoaded = true
			m.cacheHasPanes = m.hasPaneData()
			m.cacheAge = 0
		}
		if m.refreshOutput {
			m.setOutput(m.refreshSummary(msg)...)
			m.refreshOutput = false
		}
		m.syncOutputViewport(true)
		return m, nil
	case tuiOperationMsg:
		m.loading = false
		if msg.output != "" {
			m.appendOutput(msg.output)
		}
		if msg.err != nil {
			m.err = msg.err
			m.status = fmt.Sprintf("%s failed: %v", msg.label, msg.err)
			return m, nil
		}
		m.err = nil
		m.status = fmt.Sprintf("%s complete", msg.label)
		if msg.refresh {
			m.loading = true
			m.refreshModal = true
			return m, tea.Batch(loadTUIState(), m.spinner.Tick)
		}
		return m, nil
	case taskStartedMsg:
		if msg.err != nil {
			m.task = nil
			m.err = msg.err
			m.status = fmt.Sprintf("%s failed: %v", msg.spec.title, msg.err)
			m.appendOutput(fmt.Sprintf("%s failed: %v", msg.spec.title, msg.err))
			return m, nil
		}
		m.task = msg.task
		m.err = nil
		m.status = fmt.Sprintf("%s running", msg.spec.title)
		m.focus = panelOutput
		return m, tea.Batch(waitForTaskOutput(msg.task), waitForTaskDone(msg.task))
	case taskOutputMsg:
		if msg.ok {
			m.appendOutput(msg.text)
			if m.task != nil {
				return m, waitForTaskOutput(m.task)
			}
		}
		return m, nil
	case taskDoneMsg:
		if m.task != nil && m.task.spec.title == msg.spec.title {
			_ = m.task.pty.Close()
			m.task = nil
		}
		if msg.err != nil {
			m.err = msg.err
			m.status = fmt.Sprintf("%s exited: %v", msg.spec.title, msg.err)
			m.appendOutput(fmt.Sprintf("\n%s exited: %v\n", msg.spec.title, msg.err))
			if msg.spec.refresh {
				m.loading = true
				m.refreshModal = false
				return m, tea.Batch(loadTUIState(), m.spinner.Tick)
			}
			return m, nil
		}

		m.err = nil
		m.status = fmt.Sprintf("%s complete", msg.spec.title)
		m.appendOutput(fmt.Sprintf("\n%s complete\n", msg.spec.title))
		if msg.spec.refresh {
			m.loading = true
			m.refreshModal = false
			return m, tea.Batch(loadTUIState(), m.spinner.Tick)
		}
		return m, nil
	case authDoneMsg:
		m.loading = false
		if msg.err != nil {
			// Sign-in failed: leave the splash and surface the dialog so the user
			// can retry or quit.
			m.booting = false
			m.err = msg.err
			m.authDialog = true
			m.status = fmt.Sprintf("Authentication failed: %v", msg.err)
			m.setOutput(m.status)
			return m, nil
		}
		m.err = nil
		m.authDialog = false
		// Hold the splash through the full refresh that follows sign-in; the UI
		// is revealed when tuiStateMsg clears booting.
		m.booting = true
		m.status = "Authentication complete; refreshing context..."
		m.authenticated = true
		cacheApplied := m.applyPendingCacheForAuth()
		if cacheApplied {
			m.refreshModal = false
			m.refreshOutput = false
			m.setOutput()
		} else {
			m.setOutput("Authentication complete.", "Refreshing context...")
			m.refreshModal = true
			m.refreshOutput = true
		}
		m.loading = true
		return m, tea.Batch(loadTUIState(), m.spinner.Tick)
	}

	return m, nil
}

func (m tuiModel) View() string {
	width := maxInt(m.width, 80)
	height := maxInt(m.height, 24)

	if m.booting {
		return m.renderBootScreen(width, height)
	}

	if m.authDialog {
		return m.renderAuthDialog(width, height)
	}

	bodyHeight := maxInt(12, height-1)
	leftWidth := m.leftWidth(width)
	rightWidth := maxInt(30, width-leftWidth)

	left := m.renderLeft(leftWidth, bodyHeight)
	right := m.renderOutput(rightWidth, bodyHeight)
	main := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	if m.refreshModal {
		main = overlayCentered(main, m.renderRefreshModal(width), width, bodyHeight, 1)
	}
	if m.commandModal {
		main = overlayCentered(main, m.renderCommandModal(width, bodyHeight), width, bodyHeight, 2)
	}
	if m.helpModal {
		main = overlayCentered(main, m.renderHelpModal(width, bodyHeight), width, bodyHeight, 3)
	}

	return lipgloss.JoinVertical(lipgloss.Left, main, m.renderFooter(width))
}

func (m tuiModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.authDialog {
		return m.handleAuthDialogKey(msg)
	}

	if m.commandModal {
		return m.handleCommandModalKey(msg)
	}

	if m.helpModal {
		return m.handleHelpModalKey(msg)
	}

	if m.focus == panelOutput && m.task != nil && m.task.spec.interactive {
		switch msg.String() {
		case "ctrl+g":
			m.focus = panelEnvironments
			m.status = "Side panels focused"
			return m, nil
		case "ctrl+c":
			return m, sendTaskInput("\x03")
		}

		if input, ok := keyInput(msg); ok {
			return m, sendTaskInput(input)
		}
		return m, nil
	}

	if m.focus == panelOutput {
		switch msg.String() {
		case "up", "k":
			m.outputViewport.ScrollUp(1)
			return m, nil
		case "down", "j":
			m.outputViewport.ScrollDown(1)
			return m, nil
		case "pgup", "ctrl+u":
			m.outputViewport.HalfPageUp()
			return m, nil
		case "pgdown", "ctrl+d":
			m.outputViewport.HalfPageDown()
			return m, nil
		case "home", "g":
			m.outputViewport.GotoTop()
			return m, nil
		case "end", "G":
			m.outputViewport.GotoBottom()
			return m, nil
		}
	}

	switch msg.String() {
	case "ctrl+c", "q":
		m.stopTask()
		return m, tea.Quit
	case "tab", "right":
		m.moveFocus(1)
		return m, nil
	case "shift+tab", "left":
		m.moveFocus(-1)
		return m, nil
	case "1":
		m.focus = panelEnvironments
		return m, nil
	case "2":
		m.focus = panelClusters
		return m, nil
	case "3":
		m.focus = panelPods
		return m, nil
	case " ":
		m.openCommandModal()
		return m, nil
	case "?":
		m.helpModal = true
		return m, nil
	case "h":
		m.toggleHiddenItem()
		return m, nil
	case "H":
		m.toggleShowHidden()
		return m, nil
	case "0":
		m.focus = panelOutput
		return m, nil
	case "up", "k":
		m.moveCursor(-1)
		return m, nil
	case "down", "j":
		m.moveCursor(1)
		return m, nil
	case "home":
		m.cursors[m.focus] = 0
		return m, nil
	case "end":
		m.cursors[m.focus] = maxInt(0, m.itemCount(m.focus)-1)
		return m, nil
	case "r":
		m.loading = true
		m.status = "Refreshing..."
		m.refreshModal = true
		m.refreshOutput = true
		m.setOutput("Refreshing context...")
		return m, tea.Batch(loadTUIState(), m.spinner.Tick)
	case "enter":
		return m.activate()
	case "l":
		return m.runCommandByID("pod_logs")
	case "f":
		return m.runCommandByID("pod_logs_follow")
	case "s":
		return m.runCommandByID("pod_shell")
	case "c":
		return m.runCommandByID("rails_console")
	case "d":
		return m.runCommandByID("pod_describe")
	case "x":
		if m.task != nil {
			m.stopTask()
			m.status = "Stopped current task"
			m.appendOutput("\nTask stopped\n")
		}
		return m, nil
	}

	return m, nil
}

func (m tuiModel) handleAuthDialogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		return m, tea.Quit
	case "left", "right", "tab", "shift+tab":
		if m.authChoice == 0 {
			m.authChoice = 1
		} else {
			m.authChoice = 0
		}
		return m, nil
	case "a":
		m.authChoice = 0
		return m.startAuthentication()
	case "enter":
		if m.authChoice == 1 {
			return m, tea.Quit
		}
		return m.startAuthentication()
	}
	return m, nil
}

func (m tuiModel) handleCommandModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.commandItems()
	switch msg.String() {
	case "ctrl+c":
		m.stopTask()
		return m, tea.Quit
	case "q", "esc", " ":
		m.commandModal = false
		return m, nil
	case "?":
		m.commandModal = false
		m.helpModal = true
		return m, nil
	case "up", "k":
		if m.commandCursor > 0 {
			m.commandCursor--
		}
		return m, nil
	case "down", "j":
		if m.commandCursor < len(items)-1 {
			m.commandCursor++
		}
		return m, nil
	case "home":
		m.commandCursor = 0
		return m, nil
	case "end":
		m.commandCursor = maxInt(0, len(items)-1)
		return m, nil
	case "enter":
		if len(items) == 0 {
			return m, nil
		}
		m.commandModal = false
		return m.runCommandByID(items[m.commandCursor].id)
	}
	return m, nil
}

func (m tuiModel) handleHelpModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.stopTask()
		return m, tea.Quit
	case "q", "esc", "enter", "?":
		m.helpModal = false
		return m, nil
	}
	return m, nil
}

func (m *tuiModel) openCommandModal() {
	m.commandModal = true
	m.helpModal = false
	if count := len(m.commandItems()); count == 0 {
		m.commandCursor = 0
	} else if m.commandCursor >= count {
		m.commandCursor = count - 1
	}
}

func (m tuiModel) startAuthentication() (tea.Model, tea.Cmd) {
	m.authDialog = false
	m.focus = panelOutput
	m.status = "Running Google Cloud authentication..."
	m.setOutput(
		"Google Cloud authentication is running in the terminal.",
		"Complete the browser flow, then gcpeasy will refresh.",
	)
	return m, tea.Exec(tuiLoginCommand{}, func(err error) tea.Msg {
		return authDoneMsg{err: err}
	})
}

func (m tuiModel) activate() (tea.Model, tea.Cmd) {
	switch m.focus {
	case panelEnvironments:
		projects := m.visibleProjects()
		if len(projects) == 0 {
			return m, nil
		}
		project := projects[m.cursors[m.focus]]
		if project.ProjectID == m.currentProject {
			m.status = fmt.Sprintf("%s is already selected", project.ProjectID)
			return m, nil
		}
		m.loading = true
		m.status = fmt.Sprintf("Switching to %s...", project.ProjectID)
		m.selectedPod = ""
		m.setOutput(fmt.Sprintf("$ gcloud config set project %s", project.ProjectID), "")
		return m, switchProject(project)
	case panelClusters:
		clusters := m.visibleClusters()
		if len(clusters) == 0 || m.currentProject == "" {
			return m, nil
		}
		cluster := clusters[m.cursors[m.focus]]
		m.loading = true
		m.status = fmt.Sprintf("Switching to %s...", cluster.Name)
		m.selectedPod = ""
		m.setOutput(fmt.Sprintf("$ gcloud container clusters get-credentials %s --location %s --project %s", cluster.Name, cluster.Location, m.currentProject), "")
		return m, switchCluster(m.currentProject, cluster)
	case panelPods:
		pods := m.visiblePods()
		if len(pods) == 0 {
			return m, nil
		}
		pod := pods[m.cursors[m.focus]]
		m.selectedPod = podRef(pod)
		m.status = fmt.Sprintf("Selected pod %s", m.selectedPod)
		m.setOutput(
			fmt.Sprintf("Selected pod: %s", m.selectedPod),
			fmt.Sprintf("Status: %s | Ready: %s | Restarts: %s | Age: %s", pod.Status, pod.Ready, pod.Restarts, pod.Age),
		)
		return m, nil
	}

	return m, nil
}

func (m tuiModel) runCommandByID(id string) (tea.Model, tea.Cmd) {
	switch id {
	case "refresh":
		m.loading = true
		m.status = "Refreshing..."
		m.refreshModal = true
		m.refreshOutput = true
		m.setOutput("Refreshing context...")
		return m, tea.Batch(loadTUIState(), m.spinner.Tick)
	case "login":
		return m.startAuthentication()
	case "logout":
		if !m.authenticated {
			m.status = "Not authenticated"
			return m, nil
		}
		m.loading = true
		m.status = "Logging out..."
		m.selectedPod = ""
		m.setOutput("$ gcloud auth revoke <active-account>", "")
		return m, logout()
	case "reset_visibility":
		m.resetVisibility()
		m.setOutput("Visibility reset.", "Hidden environments, clusters, and pods are visible again.")
		return m, nil
	case "pod_logs":
		return m.runPodLogs(false)
	case "pod_logs_follow":
		return m.runPodLogs(true)
	case "pod_shell":
		return m.runPodShell()
	case "rails_console":
		return m.runRailsConsole()
	case "pod_describe":
		return m.runPodDescribe()
	}

	return m, nil
}

func (m tuiModel) runPodLogs(follow bool) (tea.Model, tea.Cmd) {
	pod, ok := m.activePod()
	if !ok {
		m.status = "Select a pod first"
		return m, nil
	}

	args := []string{"logs", pod.Name, "-n", pod.Namespace}
	if follow {
		args = append(args, "-f")
	}

	title := fmt.Sprintf("Logs: %s", podRef(pod))
	if follow {
		title = fmt.Sprintf("Following logs: %s", podRef(pod))
	}
	script := m.kubectlScript(args)
	m.setOutput("$ "+script, "")
	return m, startTask(shellTask(title, script, false, follow), m.outputCols(), m.outputRows())
}

func (m tuiModel) runPodDescribe() (tea.Model, tea.Cmd) {
	pod, ok := m.activePod()
	if !ok {
		m.status = "Select a pod first"
		return m, nil
	}

	args := []string{"describe", "pod", pod.Name, "-n", pod.Namespace}
	script := m.kubectlScript(args)
	m.setOutput("$ "+script, "")
	return m, startTask(shellTask(fmt.Sprintf("Describe: %s", podRef(pod)), script, false, false), m.outputCols(), m.outputRows())
}

func (m tuiModel) runPodShell() (tea.Model, tea.Cmd) {
	pod, ok := m.activePod()
	if !ok {
		m.status = "Select a pod first"
		return m, nil
	}

	script := fmt.Sprintf(
		"kubectl exec -it %s -n %s -- /bin/bash || kubectl exec -it %s -n %s -- /bin/zsh || kubectl exec -it %s -n %s -- /bin/sh",
		shQuote(pod.Name),
		shQuote(pod.Namespace),
		shQuote(pod.Name),
		shQuote(pod.Namespace),
		shQuote(pod.Name),
		shQuote(pod.Namespace),
	)
	script = m.withKubectlCredentials("(" + script + ")")
	return m.runInteractiveSession(fmt.Sprintf("Shell: %s", podRef(pod)), script)
}

func (m tuiModel) runRailsConsole() (tea.Model, tea.Cmd) {
	pod, ok := m.activePod()
	if !ok {
		m.status = "Select a pod first"
		return m, nil
	}

	consoleCommands := []string{
		"bundle exec rails console",
		"bundle exec rails c",
		"rails console",
		"rails c",
		"bin/rails console",
		"bin/rails c",
	}

	attempts := make([]string, 0, len(consoleCommands)+1)
	for _, command := range consoleCommands {
		attempts = append(
			attempts,
			fmt.Sprintf(
				"kubectl exec -it %s -n %s -- sh -lc %s",
				shQuote(pod.Name),
				shQuote(pod.Namespace),
				shQuote(command),
			),
		)
	}
	attempts = append(
		attempts,
		fmt.Sprintf("kubectl exec -it %s -n %s -- /bin/bash", shQuote(pod.Name), shQuote(pod.Namespace)),
	)

	script := m.withKubectlCredentials("(" + strings.Join(attempts, " || ") + ")")
	return m.runInteractiveSession(fmt.Sprintf("Rails console: %s", podRef(pod)), script)
}

// runInteractiveSession hands the user's real terminal to an interactive remote
// session via tea.Exec, rather than capturing it into the output viewport like
// startTask does. The TUI is suspended for the duration and restored on exit,
// so the session behaves like running kubectl directly from a normal shell.
func (m tuiModel) runInteractiveSession(title string, script string) (tea.Model, tea.Cmd) {
	m.focus = panelOutput
	m.status = fmt.Sprintf("%s — running in your terminal", title)
	m.setOutput(
		fmt.Sprintf("%s is running in your terminal.", title),
		"Type 'exit' or press Ctrl-D to return to gcpeasy.",
	)
	return m, tea.Exec(tuiInteractiveCommand{script: script}, func(err error) tea.Msg {
		return taskDoneMsg{spec: taskSpec{title: title, refresh: true}, err: err}
	})
}

func (m tuiModel) renderBootScreen(width int, height int) string {
	logoLines := compactLogoLines()
	if width >= 150 && height >= 26 {
		logoLines = wideLogoLines()
	}
	logo := renderGradientLogo(logoLines)

	status := strings.TrimSpace(m.status)
	if status == "" {
		status = "Checking authentication"
	}
	message := tuiHelpStyle.Render(fmt.Sprintf("%s %s", m.spinner.View(), status))
	detail := tuiMutedStyle.Render("Loading your GCP workspace")
	block := lipgloss.JoinVertical(lipgloss.Center, logo, "", message, detail)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, block)
}

type tuiRGB struct {
	r int
	g int
	b int
}

var tuiLogoGradient = []tuiRGB{
	{r: 174, g: 229, b: 120},
	{r: 83, g: 219, b: 190},
	{r: 82, g: 192, b: 255},
	{r: 161, g: 124, b: 255},
	{r: 255, g: 122, b: 182},
	{r: 255, g: 210, b: 102},
}

func renderGradientLogo(lines []string) string {
	maxWidth := 1
	for _, line := range lines {
		maxWidth = maxInt(maxWidth, lipgloss.Width(line))
	}

	var out strings.Builder
	for y, line := range lines {
		if y > 0 {
			out.WriteByte('\n')
		}
		column := 0
		for _, r := range line {
			if r == ' ' {
				out.WriteRune(r)
				column++
				continue
			}

			color := logoGradientColor(column, maxWidth)
			out.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(string(r)))
			column++
		}
	}
	return out.String()
}

func logoGradientColor(column int, width int) string {
	if width <= 1 || len(tuiLogoGradient) == 1 {
		return tuiLogoGradient[0].hex()
	}

	position := float64(column) / float64(width-1)
	scaled := position * float64(len(tuiLogoGradient)-1)
	index := int(scaled)
	if index >= len(tuiLogoGradient)-1 {
		return tuiLogoGradient[len(tuiLogoGradient)-1].hex()
	}

	amount := scaled - float64(index)
	return mixRGB(tuiLogoGradient[index], tuiLogoGradient[index+1], amount).hex()
}

func mixRGB(from tuiRGB, to tuiRGB, amount float64) tuiRGB {
	return tuiRGB{
		r: int(float64(from.r) + (float64(to.r)-float64(from.r))*amount),
		g: int(float64(from.g) + (float64(to.g)-float64(from.g))*amount),
		b: int(float64(from.b) + (float64(to.b)-float64(from.b))*amount),
	}
}

func (color tuiRGB) hex() string {
	return fmt.Sprintf("#%02X%02X%02X", color.r, color.g, color.b)
}

func wideLogoLines() []string {
	return []string{
		"   ggggggggg   ggggg    ccccccccccccccccppppp   ppppppppp       eeeeeeeeeeee    aaaaaaaaaaaaa      ssssssssssyyyyyyy           yyyyyyy",
		"  g:::::::::ggg::::g  cc:::::::::::::::cp::::ppp:::::::::p    ee::::::::::::ee  a::::::::::::a   ss::::::::::sy:::::y         y:::::y ",
		" g:::::::::::::::::g c:::::::::::::::::cp:::::::::::::::::p  e::::::eeeee:::::eeaaaaaaaaa:::::ass:::::::::::::sy:::::y       y:::::y  ",
		"g::::::ggggg::::::ggc:::::::cccccc:::::cpp::::::ppppp::::::pe::::::e     e:::::e         a::::as::::::ssss:::::sy:::::y     y:::::y   ",
		"g:::::g     g:::::g c::::::c     ccccccc p:::::p     p:::::pe:::::::eeeee::::::e  aaaaaaa:::::a s:::::s  ssssss  y:::::y   y:::::y    ",
		"g:::::g     g:::::g c:::::c              p:::::p     p:::::pe:::::::::::::::::e aa::::::::::::a   s::::::s        y:::::y y:::::y     ",
		"g:::::g     g:::::g c:::::c              p:::::p     p:::::pe::::::eeeeeeeeeee a::::aaaa::::::a      s::::::s      y:::::y:::::y      ",
		"g::::::g    g:::::g c::::::c     ccccccc p:::::p    p::::::pe:::::::e         a::::a    a:::::assssss   s:::::s     y:::::::::y       ",
		"g:::::::ggggg:::::g c:::::::cccccc:::::c p:::::ppppp:::::::pe::::::::e        a::::a    a:::::as:::::ssss::::::s     y:::::::y        ",
		" g::::::::::::::::g  c:::::::::::::::::c p::::::::::::::::p  e::::::::eeeeeeeea:::::aaaa::::::as::::::::::::::s       y:::::y         ",
		"  gg::::::::::::::g   cc:::::::::::::::c p::::::::::::::pp    ee:::::::::::::e a::::::::::aa:::as:::::::::::ss       y:::::y          ",
		"    gggggggg::::::g     cccccccccccccccc p::::::pppppppp        eeeeeeeeeeeeee  aaaaaaaaaa  aaaa sssssssssss        y:::::y           ",
		"            g:::::g                      p:::::p                                                                   y:::::y            ",
		"gggggg      g:::::g                      p:::::p                                                                  y:::::y             ",
		"g:::::gg   gg:::::g                     p:::::::p                                                                y:::::y              ",
		" g::::::ggg:::::::g                     p:::::::p                                                               y:::::y               ",
		"  gg:::::::::::::g                      p:::::::p                                                              yyyyyyy                ",
		"    ggg::::::ggg                        ppppppppp                                                                                     ",
		"       gggggg                                                                                                                         ",
	}
}

func compactLogoLines() []string {
	return []string{
		"  ____  ____ ____  _____    _    ______   __",
		" / ___|/ ___|  _ \\| ____|  / \\  / ___\\ \\ / /",
		"| |  _| |   | |_) |  _|   / _ \\ \\___ \\\\ V / ",
		"| |_| | |___|  __/| |___ / ___ \\ ___) || |  ",
		" \\____|\\____|_|   |_____/_/   \\_\\____/ |_|  ",
		"GCPEASY",
	}
}

func (m tuiModel) renderAuthDialog(width int, height int) string {
	dialogWidth := minInt(68, maxInt(44, width-12))
	body := lipgloss.JoinVertical(
		lipgloss.Left,
		tuiTitleStyle.Render("Google Cloud authentication required"),
		"",
		"gcpeasy needs an active gcloud session before it can list projects, clusters, and pods.",
		"",
		tuiMutedStyle.Render("Authenticate launches the normal browser login flow."),
		"",
		m.renderAuthButtons(),
		"",
		tuiHelpStyle.Render("enter choose | tab switch | q quit"),
	)

	box := lipgloss.NewStyle().
		Width(dialogWidth).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tuiActiveBorderColor).
		Render(body)

	topPad := maxInt(0, (height-lipgloss.Height(box))/2)
	leftPad := maxInt(0, (width-lipgloss.Width(box))/2)

	return strings.Repeat("\n", topPad) + lipgloss.NewStyle().MarginLeft(leftPad).Render(box)
}

func (m tuiModel) renderAuthButtons() string {
	authButton := tuiButtonStyle.Render("Authenticate")
	quitButton := tuiButtonStyle.Render("Quit")
	if m.authChoice == 0 {
		authButton = tuiActiveButtonStyle.Render("Authenticate")
	} else {
		quitButton = tuiActiveButtonStyle.Render("Quit")
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, authButton, "  ", quitButton)
}

func overlayCentered(base string, modal string, width int, height int, z int) string {
	modalWidth, modalHeight := lipgloss.Size(modal)
	x := maxInt(0, (width-modalWidth)/2)
	y := maxInt(0, (height-modalHeight)/2)

	canvas := lipgloss.NewCanvas(width, height)
	composite := lipgloss.NewCompositor(
		lipgloss.NewLayer(base).X(0).Y(0).Z(0),
		lipgloss.NewLayer(modal).X(x).Y(y).Z(z),
	)
	return canvas.Compose(composite).Render()
}

func (m tuiModel) renderRefreshModal(width int) string {
	modalWidth := minInt(88, maxInt(64, width*3/5))
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("81")).
		Render("Refreshing context")

	activity := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("149")).
		Render(m.spinner.View())

	message := "Checking gcloud and kubectl state"
	detail := "Loading environments, clusters, and pods."
	if m.cacheLoaded && m.cacheHasPanes {
		message = fmt.Sprintf("Refreshing context; cached panes are visible (%s old)", formatDuration(m.cacheAge))
		detail = "You can keep working while the background refresh completes."
	}

	body := lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Center, activity, "  ", title),
		"",
		message,
		tuiMutedStyle.Render(detail),
	)

	return lipgloss.NewStyle().
		Width(modalWidth).
		Padding(1, 3).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tuiActiveBorderColor).
		Align(lipgloss.Left).
		Render(body)
}

func (m tuiModel) renderLeft(width int, height int) string {
	statusHeight := 6
	selectorHeight := maxInt(5, (height-statusHeight)/3)
	podsHeight := maxInt(5, height-statusHeight-selectorHeight*2)

	panels := []string{
		m.renderStatusPanel(width, statusHeight),
		m.renderEnvironmentPanel(width, selectorHeight),
		m.renderClusterPanel(width, selectorHeight),
		m.renderPodPanel(width, podsHeight),
	}
	return lipgloss.JoinVertical(lipgloss.Left, panels...)
}

func (m tuiModel) renderStatusPanel(width int, height int) string {
	lines := []string{
		fmt.Sprintf("auth    %s", yesNo(m.authenticated)),
		fmt.Sprintf("project %s", fallback(m.currentProject, "none")),
		fmt.Sprintf("cluster %s", fallback(shortContext(m.currentCluster), "none")),
		fmt.Sprintf("pod     %s", fallback(m.selectedPod, "none")),
	}
	if m.loading && m.refreshModal {
		lines = append(lines, tuiMutedStyle.Render("refreshing..."))
	}
	if m.cacheLoaded {
		label := "state"
		if m.cacheHasPanes {
			label = "cache"
		}
		lines = append(lines, tuiMutedStyle.Render(fmt.Sprintf("%-7s%s old", label, formatDuration(m.cacheAge))))
	}
	return m.renderPanel("[S]-Status", panelOutput+1, width, height, strings.Join(lines, "\n"))
}

func (m tuiModel) renderEnvironmentPanel(width int, height int) string {
	contentHeight := panelContentHeight(height)
	title := m.panelTitle("[1]-Environments")
	if !m.authenticated {
		return m.renderPanel(title, panelEnvironments, width, height, tuiMutedStyle.Render("Not authenticated"))
	}
	if len(m.projects) == 0 {
		return m.renderPanel(title, panelEnvironments, width, height, tuiMutedStyle.Render("No environments found"))
	}

	projects := m.visibleProjects()
	if len(projects) == 0 {
		return m.renderPanel(title, panelEnvironments, width, height, tuiMutedStyle.Render("All environments hidden. Press H to show hidden items."))
	}

	rows := make([]string, 0, len(projects))
	for i, project := range projects {
		marker := " "
		if project.ProjectID == m.currentProject {
			marker = "*"
		}
		hidden := m.isProjectHidden(project)
		rows = append(rows, m.renderRow(panelEnvironments, i, marker, project.ProjectID, m.detailWithHidden(project.Name, hidden), width, hidden))
	}
	return m.renderPanel(title, panelEnvironments, width, height, strings.Join(visibleRows(rows, m.cursors[panelEnvironments], contentHeight), "\n"))
}

func (m tuiModel) renderClusterPanel(width int, height int) string {
	contentHeight := panelContentHeight(height)
	title := m.panelTitle("[2]-Clusters")
	if !m.authenticated {
		return m.renderPanel(title, panelClusters, width, height, tuiMutedStyle.Render("Not authenticated"))
	}
	if m.currentProject == "" {
		return m.renderPanel(title, panelClusters, width, height, tuiMutedStyle.Render("Select an environment"))
	}
	if len(m.clusters) == 0 {
		return m.renderPanel(title, panelClusters, width, height, tuiMutedStyle.Render("No clusters found"))
	}

	clusters := m.visibleClusters()
	if len(clusters) == 0 {
		return m.renderPanel(title, panelClusters, width, height, tuiMutedStyle.Render("All clusters hidden. Press H to show hidden items."))
	}

	rows := make([]string, 0, len(clusters))
	for i, cluster := range clusters {
		marker := " "
		if isCurrentCluster(cluster, m.currentCluster) {
			marker = "*"
		}
		hidden := m.isClusterHidden(cluster)
		rows = append(rows, m.renderRow(panelClusters, i, marker, cluster.Name, m.detailWithHidden(cluster.Location, hidden), width, hidden))
	}
	return m.renderPanel(title, panelClusters, width, height, strings.Join(visibleRows(rows, m.cursors[panelClusters], contentHeight), "\n"))
}

func (m tuiModel) renderPodPanel(width int, height int) string {
	contentHeight := panelContentHeight(height)
	title := m.panelTitle("[3]-Pods")
	if !m.authenticated {
		return m.renderPanel(title, panelPods, width, height, tuiMutedStyle.Render("Not authenticated"))
	}
	if m.currentCluster == "" {
		return m.renderPanel(title, panelPods, width, height, tuiMutedStyle.Render("Select a cluster"))
	}
	if len(m.pods) == 0 {
		return m.renderPanel(title, panelPods, width, height, tuiMutedStyle.Render("No application pods found"))
	}

	pods := m.visiblePods()
	if len(pods) == 0 {
		return m.renderPanel(title, panelPods, width, height, tuiMutedStyle.Render("All pods hidden. Press H to show hidden items."))
	}

	rows := make([]string, 0, len(pods))
	for i, pod := range pods {
		marker := " "
		if podRef(pod) == m.selectedPod {
			marker = "*"
		}
		detail := fmt.Sprintf("%s %s %s", pod.Status, pod.Ready, pod.Age)
		hidden := m.isPodHidden(pod)
		rows = append(rows, m.renderRow(panelPods, i, marker, podRef(pod), m.detailWithHidden(detail, hidden), width, hidden))
	}
	return m.renderPanel(title, panelPods, width, height, strings.Join(visibleRows(rows, m.cursors[panelPods], contentHeight), "\n"))
}

func (m tuiModel) renderCommandModal(width int, height int) string {
	items := m.commandItems()
	modalWidth := minInt(82, maxInt(52, width/2))
	contentHeight := maxInt(6, minInt(len(items)+1, height-8))
	rows := make([]string, 0, len(items)+1)
	for i, item := range items {
		detail := item.description
		titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
		if !item.enabled {
			detail = item.reason
			titleStyle = tuiMutedStyle
		}
		cursor := " "
		if i == m.commandCursor {
			cursor = ">"
		}
		line := fmt.Sprintf("%s %s", cursor, titleStyle.Render(item.title))
		remaining := modalWidth - lipgloss.Width(line) - 8
		if detail != "" && remaining > 8 {
			line += tuiMutedStyle.Render("  " + clip(detail, remaining))
		}
		if i == m.commandCursor {
			rowWidth := maxInt(1, modalWidth-8)
			line = tuiSelectedRowStyle.Width(rowWidth).MaxWidth(rowWidth).Render(line)
		}
		rows = append(rows, line)
	}

	body := lipgloss.JoinVertical(
		lipgloss.Left,
		tuiTitleStyle.Render("Command palette"),
		strings.Join(visibleRows(rows, m.commandCursor, contentHeight), "\n"),
		"",
		tuiHelpStyle.Render("enter run | j/k move | esc close"),
	)

	return lipgloss.NewStyle().
		Width(modalWidth).
		Padding(1, 3).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tuiActiveBorderColor).
		Render(body)
}

func (m tuiModel) renderHelpModal(width int, height int) string {
	modalWidth := minInt(78, maxInt(52, width/2))
	rows := []string{
		helpRow("tab / shift+tab", "Move focus between side panes and output"),
		helpRow("1 / 2 / 3", "Focus environments, clusters, or pods"),
		helpRow("0", "Focus task output"),
		helpRow("space", "Open command palette"),
		helpRow("?", "Open or close this help"),
		helpRow("j / k", "Move selection or scroll output"),
		helpRow("enter", "Run the primary action for the focused pane"),
		helpRow("h", "Hide or unhide the selected environment, cluster, or pod"),
		helpRow("H", "Toggle showing hidden items"),
		helpRow("r", "Refresh context"),
		helpRow("q", "Quit"),
	}

	body := lipgloss.JoinVertical(
		lipgloss.Left,
		tuiTitleStyle.Render("Help"),
		strings.Join(rows, "\n"),
		"",
		tuiHelpStyle.Render("esc close"),
	)

	return lipgloss.NewStyle().
		Width(modalWidth).
		Padding(1, 3).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tuiActiveBorderColor).
		Render(body)
}

func (m tuiModel) renderOutput(width int, height int) string {
	contentHeight := panelContentHeight(height)
	rendered := make([]string, 0, 2)
	if m.task != nil {
		rendered = append(rendered, tuiSuccessStyle.Render("Running: "+m.task.spec.title))
	} else if m.err != nil {
		rendered = append(rendered, tuiErrorStyle.Render(m.status))
	} else {
		rendered = append(rendered, tuiMutedStyle.Render(m.status))
	}

	vp := m.outputViewport
	vp.Width = maxInt(1, panelInnerWidth(width))
	vp.Height = maxInt(1, contentHeight-1)
	vp.SetContent(m.outputContent(vp.Width, vp.Height))
	rendered = append(rendered, vp.View())

	return m.renderPanel("[0]-Task output", panelOutput, width, height, strings.Join(rendered, "\n"))
}

func (m tuiModel) renderPanel(title string, panel tuiPanel, width int, height int, content string) string {
	borderColor := tuiInactiveBorderColor
	titleStyle := tuiMutedStyle
	if panel == m.focus {
		borderColor = tuiActiveBorderColor
		titleStyle = tuiPanelTitleStyle
	}

	if height < 3 {
		height = 3
	}

	innerWidth := panelInnerWidth(width)
	innerHeight := maxInt(1, height-2)
	contentLines := strings.Split(content, "\n")
	lines := make([]string, 0, innerHeight)
	lines = append(lines, titleStyle.Render(title))

	for i := 0; i < innerHeight-1; i++ {
		line := ""
		if i < len(contentLines) {
			line = clip(contentLines[i], innerWidth)
		}
		lines = append(lines, line)
	}

	return lipgloss.NewStyle().
		Width(maxInt(12, width)).
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		BorderTop(true).
		BorderBottom(true).
		BorderLeft(true).
		BorderRight(true).
		Render(strings.Join(lines, "\n"))
}

func (m tuiModel) renderRow(panel tuiPanel, index int, marker string, primary string, secondary string, width int, hidden bool) string {
	rowWidth := panelInnerWidth(width)
	selected := panel == m.focus && index == m.cursors[panel]
	cursor := " "
	if selected {
		cursor = ">"
	}

	line := fmt.Sprintf("%s %s %s", cursor, marker, primary)
	remaining := rowWidth - lipgloss.Width(line) - 2
	if secondary != "" && remaining > 8 {
		detail := "  " + clip(secondary, remaining)
		if selected || hidden {
			line += detail
		} else {
			line += tuiMutedStyle.Render(detail)
		}
	}

	if selected {
		line = clip(line, rowWidth)
		if padding := rowWidth - lipgloss.Width(line); padding > 0 {
			line += strings.Repeat(" ", padding)
		}
		if hidden {
			return tuiHiddenSelectedRowStyle.Render(line)
		}
		return tuiSelectedRowStyle.Render(line)
	}
	line = clip(line, rowWidth)
	if hidden {
		return tuiHiddenRowStyle.Render(line)
	}
	return line
}

func (m tuiModel) renderFooter(width int) string {
	style := tuiHelpStyle
	if m.err != nil {
		style = tuiErrorStyle
	}
	gutter := 1
	innerWidth := maxInt(1, width-gutter*2)
	hints := strings.Join(m.footerHints(), " | ")
	indicator := m.backgroundIndicator()
	if indicator == "" {
		return padFooter(style.Render(clip(hints, innerWidth)), width, gutter)
	}

	indicator = tuiHelpStyle.Render(indicator)
	indicatorWidth := lipgloss.Width(indicator)
	leftWidth := maxInt(0, innerWidth-indicatorWidth-1)
	left := style.Render(clip(hints, leftWidth))
	gap := maxInt(1, innerWidth-lipgloss.Width(left)-indicatorWidth)
	return padFooter(left+strings.Repeat(" ", gap)+indicator, width, gutter)
}

func padFooter(content string, width int, gutter int) string {
	if width <= 0 {
		return ""
	}
	if width <= gutter*2 {
		return strings.Repeat(" ", width)
	}

	innerWidth := width - gutter*2
	rightPad := maxInt(0, innerWidth-lipgloss.Width(content))
	return strings.Repeat(" ", gutter) + content + strings.Repeat(" ", rightPad+gutter)
}

func (m tuiModel) backgroundIndicator() string {
	if !m.loading || m.refreshModal {
		return ""
	}
	return fmt.Sprintf("%s refreshing", m.spinner.View())
}

func (m tuiModel) footerHints() []string {
	if m.commandModal {
		return []string{footerHint("Run", "enter"), footerHint("Move", "j/k"), footerHint("Close", "esc"), footerHint("Quit", "ctrl+c")}
	}

	if m.helpModal {
		return []string{footerHint("Close", "esc/q"), footerHint("Quit", "ctrl+c")}
	}

	if m.focus == panelOutput && m.task != nil && m.task.spec.interactive {
		return []string{
			footerHint("Input", "typing"),
			footerHint("Side panels", "ctrl+g"),
			footerHint("Interrupt", "ctrl+c"),
			footerHint("Stop", "x"),
		}
	}

	base := []string{footerHint("Commands", "<space>"), footerHint("Help", "?")}

	switch m.focus {
	case panelEnvironments:
		projects := m.visibleProjects()
		if !m.authenticated {
			return append(base, footerHint("Noop", "enter"), footerHint("Quit", "q"))
		}
		if len(projects) == 0 {
			return append(base, footerHint("Refresh", "r"), footerHint("Quit", "q"))
		}
		project := projects[m.cursors[panelEnvironments]]
		action := footerHint("Switch env", "enter")
		if project.ProjectID == m.currentProject {
			action = footerHint("Already active", "enter")
		}
		return append(base, action, footerHint("Refresh", "r"), footerHint("Quit", "q"))
	case panelClusters:
		clusters := m.visibleClusters()
		if len(clusters) == 0 {
			return append(base, footerHint("Select env first", "enter"), footerHint("Refresh", "r"), footerHint("Quit", "q"))
		}
		cluster := clusters[m.cursors[panelClusters]]
		action := footerHint("Switch cluster", "enter")
		if isCurrentCluster(cluster, m.currentCluster) {
			action = footerHint("Already active", "enter")
		}
		return append(base, action, footerHint("Refresh", "r"), footerHint("Quit", "q"))
	case panelPods:
		if len(m.visiblePods()) == 0 {
			return append(base, footerHint("Select cluster first", "enter"), footerHint("Refresh", "r"), footerHint("Quit", "q"))
		}
		return append(base, footerHint("Select", "enter"), footerHint("Logs", "l"), footerHint("Follow", "f"), footerHint("Shell", "s"), footerHint("Console", "c"), footerHint("Describe", "d"), footerHint("Quit", "q"))
	case panelOutput:
		if m.task != nil {
			return append(base, footerHint("Stop task", "x"), footerHint("Side panels", "ctrl+g"), footerHint("Quit", "q"))
		}
		return append(base, footerHint("Scroll", "j/k"), footerHint("Page", "pgup/pgdn"), footerHint("Refresh", "r"), footerHint("Quit", "q"))
	default:
		return append(base, footerHint("Quit", "q"))
	}
}

func footerHint(action string, key string) string {
	return fmt.Sprintf("%s: %s", action, key)
}

func (m tuiModel) commandItems() []tuiCommandItem {
	items := []tuiCommandItem{
		{
			id:          "refresh",
			title:       "Refresh context",
			description: "Reload auth, environment, cluster, and pod state",
			enabled:     true,
		},
	}
	if m.authenticated {
		items = append(items, tuiCommandItem{
			id:          "logout",
			title:       "Logout from Google Cloud",
			description: "Revoke the active gcloud account",
			enabled:     true,
		})
	} else {
		items = append(items, tuiCommandItem{
			id:          "login",
			title:       "Login to Google Cloud",
			description: "Run the browser authentication flow",
			enabled:     true,
		})
	}
	items = append(items, tuiCommandItem{
		id:          "reset_visibility",
		title:       "Reset visibility",
		description: "Show all hidden environments, clusters, and pods",
		enabled:     true,
	})
	return items
}

func helpRow(keys string, description string) string {
	key := tuiPanelTitleStyle.Render(keys)
	gap := strings.Repeat(" ", maxInt(1, 18-lipgloss.Width(keys)))
	return key + gap + tuiMutedStyle.Render(description)
}

func (m tuiModel) panelTitle(title string) string {
	if m.showHidden {
		return title + " (all)"
	}
	return title
}

func (m tuiModel) detailWithHidden(detail string, hidden bool) string {
	if !hidden {
		return detail
	}
	if strings.TrimSpace(detail) == "" {
		return "[hidden]"
	}
	return detail + " [hidden]"
}

func (m tuiModel) visibleProjects() []GCPProject {
	if m.showHidden {
		return m.projects
	}

	projects := make([]GCPProject, 0, len(m.projects))
	for _, project := range m.projects {
		if !m.isProjectHidden(project) {
			projects = append(projects, project)
		}
	}
	return projects
}

func (m tuiModel) visibleClusters() []internal.ClusterInfo {
	if m.showHidden {
		return m.clusters
	}

	clusters := make([]internal.ClusterInfo, 0, len(m.clusters))
	for _, cluster := range m.clusters {
		if !m.isClusterHidden(cluster) {
			clusters = append(clusters, cluster)
		}
	}
	return clusters
}

func (m tuiModel) visiblePods() []internal.PodInfo {
	if m.showHidden {
		return m.pods
	}

	pods := make([]internal.PodInfo, 0, len(m.pods))
	for _, pod := range m.pods {
		if !m.isPodHidden(pod) {
			pods = append(pods, pod)
		}
	}
	return pods
}

func (m tuiModel) isProjectHidden(project GCPProject) bool {
	return m.hiddenEnvironments[project.ProjectID]
}

func (m tuiModel) isClusterHidden(cluster internal.ClusterInfo) bool {
	return m.hiddenClusters[m.clusterKey(cluster)]
}

func (m tuiModel) isPodHidden(pod internal.PodInfo) bool {
	return m.hiddenPods[m.podKey(pod)]
}

func (m tuiModel) clusterKey(cluster internal.ClusterInfo) string {
	return strings.Join([]string{m.currentProject, cluster.Location, cluster.Name}, "\x1f")
}

func (m tuiModel) podKey(pod internal.PodInfo) string {
	return strings.Join([]string{m.currentProject, m.currentCluster, pod.Namespace, pod.Name}, "\x1f")
}

func (m *tuiModel) toggleShowHidden() {
	m.showHidden = !m.showHidden
	if m.showHidden {
		m.status = "Showing hidden items"
	} else {
		m.status = "Hiding hidden items"
	}
	m.clampCursors()
}

func (m *tuiModel) toggleHiddenItem() {
	m.ensureHiddenMaps()
	switch m.focus {
	case panelEnvironments:
		projects := m.visibleProjects()
		if len(projects) == 0 {
			m.status = "No environment selected"
			return
		}
		project := projects[m.cursors[panelEnvironments]]
		hidden := toggleHidden(m.hiddenEnvironments, project.ProjectID)
		m.status = hiddenStatus("environment", project.ProjectID, hidden)
	case panelClusters:
		clusters := m.visibleClusters()
		if len(clusters) == 0 {
			m.status = "No cluster selected"
			return
		}
		cluster := clusters[m.cursors[panelClusters]]
		hidden := toggleHidden(m.hiddenClusters, m.clusterKey(cluster))
		m.status = hiddenStatus("cluster", cluster.Name, hidden)
	case panelPods:
		pods := m.visiblePods()
		if len(pods) == 0 {
			m.status = "No pod selected"
			return
		}
		pod := pods[m.cursors[panelPods]]
		hidden := toggleHidden(m.hiddenPods, m.podKey(pod))
		if hidden && m.selectedPod == podRef(pod) {
			m.selectedPod = ""
		}
		m.status = hiddenStatus("pod", podRef(pod), hidden)
	default:
		m.status = "Select an environment, cluster, or pod first"
		return
	}

	m.savePreferences()
	m.clampCursors()
}

func (m *tuiModel) resetVisibility() {
	m.hiddenEnvironments = map[string]bool{}
	m.hiddenClusters = map[string]bool{}
	m.hiddenPods = map[string]bool{}
	m.showHidden = false
	m.status = "Visibility reset"
	if err := clearTUIPreferences(); err != nil {
		m.err = err
		m.status = fmt.Sprintf("Unable to reset visibility: %v", err)
	}
	m.clampCursors()
}

func (m *tuiModel) ensureHiddenMaps() {
	if m.hiddenEnvironments == nil {
		m.hiddenEnvironments = map[string]bool{}
	}
	if m.hiddenClusters == nil {
		m.hiddenClusters = map[string]bool{}
	}
	if m.hiddenPods == nil {
		m.hiddenPods = map[string]bool{}
	}
}

func toggleHidden(hidden map[string]bool, key string) bool {
	if hidden[key] {
		delete(hidden, key)
		return false
	}
	hidden[key] = true
	return true
}

func hiddenStatus(kind string, name string, hidden bool) string {
	if hidden {
		return fmt.Sprintf("Hid %s %s", kind, name)
	}
	return fmt.Sprintf("Unhid %s %s", kind, name)
}

func (m tuiModel) activePod() (internal.PodInfo, bool) {
	if len(m.pods) == 0 {
		return internal.PodInfo{}, false
	}
	if m.focus == panelPods {
		pods := m.visiblePods()
		cursor := m.cursors[panelPods]
		if cursor >= 0 && cursor < len(pods) {
			return pods[cursor], true
		}
		return internal.PodInfo{}, false
	}
	if m.selectedPod != "" {
		for _, pod := range m.pods {
			if podRef(pod) == m.selectedPod {
				return pod, true
			}
		}
	}

	cursor := m.cursors[panelPods]
	if cursor < 0 || cursor >= len(m.pods) {
		return internal.PodInfo{}, false
	}
	return m.pods[cursor], true
}

func (m tuiModel) activeCluster() (internal.ClusterInfo, bool) {
	for _, cluster := range m.clusters {
		if isCurrentCluster(cluster, m.currentCluster) {
			return cluster, true
		}
	}
	if len(m.clusters) == 1 {
		return m.clusters[0], true
	}
	return internal.ClusterInfo{}, false
}

func (m tuiModel) kubectlScript(args []string) string {
	return m.withKubectlCredentials(shellCommand("kubectl", args))
}

func (m tuiModel) withKubectlCredentials(command string) string {
	cluster, ok := m.activeCluster()
	if !ok || strings.TrimSpace(m.currentProject) == "" {
		return command
	}
	return credentialRefreshScript(m.currentProject, cluster) + " && " + command
}

func credentialRefreshScript(projectID string, cluster internal.ClusterInfo) string {
	return shellCommand("gcloud", []string{
		"container",
		"clusters",
		"get-credentials",
		cluster.Name,
		"--location",
		cluster.Location,
		"--project",
		projectID,
	}) + " >/dev/null"
}

func (m *tuiModel) validateSelectedPod() {
	if m.selectedPod == "" {
		return
	}
	for _, pod := range m.pods {
		if podRef(pod) == m.selectedPod {
			return
		}
	}
	m.selectedPod = ""
}

func (m *tuiModel) applyCache(cache tuiStateCache) {
	m.authenticated = cache.Authenticated
	m.currentProject = cache.CurrentProject
	m.currentCluster = cache.CurrentCluster
	m.selectedPod = cache.SelectedPod
	m.projects = append([]GCPProject(nil), cache.Projects...)
	m.clusters = append([]internal.ClusterInfo(nil), cache.Clusters...)
	m.pods = append([]internal.PodInfo(nil), cache.Pods...)
	m.validateSelectedPod()
	m.clampCursors()
}

func (m *tuiModel) applyPendingCache(cache tuiStateCache, msg tuiStateMsg) bool {
	if !cacheHasUsefulData(cache) {
		return false
	}
	if strings.TrimSpace(cache.CurrentProject) != "" && cache.CurrentProject != msg.currentProject {
		return false
	}
	if strings.TrimSpace(cache.CurrentCluster) != "" && msg.currentCluster != "" && cache.CurrentCluster != msg.currentCluster {
		return false
	}

	if !msg.projectsLoaded {
		m.projects = append([]GCPProject(nil), cache.Projects...)
	}
	if !msg.clustersLoaded {
		m.clusters = append([]internal.ClusterInfo(nil), cache.Clusters...)
	}
	if !msg.podsLoaded {
		m.pods = append([]internal.PodInfo(nil), cache.Pods...)
	}
	if m.selectedPod == "" {
		m.selectedPod = cache.SelectedPod
	}
	m.cacheLoaded = true
	m.cacheHasPanes = cacheHasPaneData(cache)
	m.cacheAge = time.Since(cache.CachedAt)
	return true
}

func (m *tuiModel) applyPendingCacheForAuth() bool {
	m.retainPendingCache()
	if m.pendingCache == nil {
		return false
	}

	cache := *m.pendingCache
	if strings.TrimSpace(m.currentProject) == "" {
		m.currentProject = cache.CurrentProject
	}
	if strings.TrimSpace(m.currentCluster) == "" {
		m.currentCluster = cache.CurrentCluster
	}

	applied := m.applyPendingCache(cache, tuiStateMsg{
		authenticated:  true,
		currentProject: m.currentProject,
		currentCluster: m.currentCluster,
	})
	if applied {
		m.pendingCache = nil
	}
	return applied
}

func (m *tuiModel) retainPendingCache() {
	if m.pendingCache != nil && cacheHasUsefulData(*m.pendingCache) {
		return
	}

	if m.hasPaneData() ||
		strings.TrimSpace(m.currentProject) != "" ||
		strings.TrimSpace(m.currentCluster) != "" ||
		strings.TrimSpace(m.selectedPod) != "" {
		cache := tuiStateCache{
			Authenticated:  true,
			CurrentProject: m.currentProject,
			CurrentCluster: m.currentCluster,
			SelectedPod:    m.selectedPod,
			Projects:       append([]GCPProject(nil), m.projects...),
			Clusters:       append([]internal.ClusterInfo(nil), m.clusters...),
			Pods:           append([]internal.PodInfo(nil), m.pods...),
			CachedAt:       time.Now(),
		}
		m.pendingCache = &cache
		return
	}

	if cache, ok := loadTUIStateCache(); ok && cacheHasUsefulData(cache) {
		m.pendingCache = &cache
	}
}

func (m *tuiModel) clearCachedContext() {
	m.currentProject = ""
	m.currentCluster = ""
	m.selectedPod = ""
	m.projects = nil
	m.clusters = nil
	m.pods = nil
	m.cacheLoaded = false
	m.cacheHasPanes = false
	m.cacheAge = 0
	m.clampCursors()
}

func (m *tuiModel) applyPreferences(preferences tuiPreferences) {
	m.hiddenEnvironments = hiddenSet(preferences.HiddenEnvironments)
	m.hiddenClusters = hiddenSet(preferences.HiddenClusters)
	m.hiddenPods = hiddenSet(preferences.HiddenPods)
}

func (m tuiModel) saveCache() {
	cache := tuiStateCache{
		Authenticated:  m.authenticated,
		CurrentProject: m.currentProject,
		CurrentCluster: m.currentCluster,
		SelectedPod:    m.selectedPod,
		Projects:       m.projects,
		Clusters:       m.clusters,
		Pods:           m.pods,
		CachedAt:       time.Now(),
	}
	_ = saveTUIStateCache(cache)
}

func (m tuiModel) savePreferences() {
	preferences := tuiPreferences{
		HiddenEnvironments: hiddenKeys(m.hiddenEnvironments),
		HiddenClusters:     hiddenKeys(m.hiddenClusters),
		HiddenPods:         hiddenKeys(m.hiddenPods),
	}
	_ = saveTUIPreferences(preferences)
}

func (m *tuiModel) moveFocus(delta int) {
	next := int(m.focus) + delta
	if next < 0 {
		next = int(panelCount) - 1
	}
	if next >= int(panelCount) {
		next = 0
	}
	m.focus = tuiPanel(next)
	m.clampCursors()
}

func (m *tuiModel) moveCursor(delta int) {
	count := m.itemCount(m.focus)
	if count == 0 {
		m.cursors[m.focus] = 0
		return
	}

	next := m.cursors[m.focus] + delta
	if next < 0 {
		next = 0
	}
	if next >= count {
		next = count - 1
	}
	m.cursors[m.focus] = next
}

func (m *tuiModel) clampCursors() {
	for panel := tuiPanel(0); panel < panelCount; panel++ {
		count := m.itemCount(panel)
		if count == 0 {
			m.cursors[panel] = 0
			continue
		}
		if m.cursors[panel] >= count {
			m.cursors[panel] = count - 1
		}
		if m.cursors[panel] < 0 {
			m.cursors[panel] = 0
		}
	}
}

func (m tuiModel) itemCount(panel tuiPanel) int {
	switch panel {
	case panelEnvironments:
		return len(m.visibleProjects())
	case panelClusters:
		return len(m.visibleClusters())
	case panelPods:
		return len(m.visiblePods())
	default:
		return 0
	}
}

func (m tuiModel) hasPaneData() bool {
	return len(m.projects) > 0 || len(m.clusters) > 0 || len(m.pods) > 0
}

func (m tuiModel) outputContent(width int, height int) string {
	if len(m.output) == 0 || (len(m.output) == 1 && strings.TrimSpace(m.output[0]) == "") {
		return renderIdleOutput(width, height)
	}
	return strings.Join(m.output, "\n")
}

func (m tuiModel) refreshSummary(msg tuiStateMsg) []string {
	if msg.err != nil {
		return []string{
			"Unable to refresh context.",
			msg.err.Error(),
		}
	}
	if !msg.authenticated {
		return []string{
			"Authentication required.",
			"Open the command palette and choose Login to Google Cloud.",
		}
	}

	lines := []string{
		"Context refreshed.",
		fmt.Sprintf("Project: %s", fallback(m.currentProject, "none")),
		fmt.Sprintf("Cluster: %s", fallback(shortContext(m.currentCluster), "none")),
		fmt.Sprintf("Loaded: %d environments, %d clusters, %d pods", len(m.projects), len(m.clusters), len(m.pods)),
	}
	if len(msg.warnings) > 0 {
		lines = append(lines, "", "Warnings:")
		lines = append(lines, msg.warnings...)
	}
	return lines
}

func renderIdleOutput(width int, height int) string {
	title := renderGradientLogo(idleLogoLines(width, height))
	hint := tuiMutedStyle.Render("Select a pod, open the command palette, or run a task.")
	block := lipgloss.JoinVertical(lipgloss.Center, title, "", hint)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, block)
}

func idleLogoLines(width int, height int) []string {
	wide := wideLogoLines()
	if width >= logoWidth(wide) && height >= len(wide)+4 {
		return wide
	}

	medium := mediumLogoLines()
	if width >= logoWidth(medium) && height >= len(medium)+4 {
		return medium
	}

	return compactLogoLines()
}

func logoWidth(lines []string) int {
	width := 0
	for _, line := range lines {
		width = maxInt(width, lipgloss.Width(line))
	}
	return width
}

func mediumLogoLines() []string {
	return []string{
		"  ggggg    ccccc  ppppp    eeeee   aaaaa   sssss  yy   yy",
		" gg   gg  cc      pp  pp  ee      aa   aa ss       yy yy ",
		"gg        cc      pp  pp  eeeee   aaaaaaa  ssss     yyy  ",
		"gg  gggg  cc      ppppp   ee      aa   aa     ss    yyy  ",
		" gg  gg   cc      pp      ee      aa   aa     ss    yyy  ",
		"  ggggg    ccccc  pp       eeeee  aa   aa sssss     yyy  ",
		"     gg                                             yy    ",
		"  gggg                                           yyyy     ",
	}
}

func (m *tuiModel) syncOutputViewport(follow bool) {
	bodyHeight := maxInt(12, maxInt(m.height, 24)-1)
	leftWidth := m.leftWidth(maxInt(m.width, 80))
	rightWidth := maxInt(30, maxInt(m.width, 80)-leftWidth)
	contentHeight := panelContentHeight(bodyHeight)

	wasAtBottom := m.outputViewport.AtBottom()
	m.outputViewport.Width = maxInt(1, panelInnerWidth(rightWidth))
	m.outputViewport.Height = maxInt(1, contentHeight-1)
	m.outputViewport.SetContent(m.outputContent(m.outputViewport.Width, m.outputViewport.Height))
	if follow || wasAtBottom {
		m.outputViewport.GotoBottom()
	}
}

func (m *tuiModel) setOutput(lines ...string) {
	if len(lines) == 0 {
		lines = []string{""}
	}
	m.output = append([]string(nil), lines...)
	m.outputRow = len(m.output) - 1
	m.outputCol = runeLen(m.output[m.outputRow])
	m.syncOutputViewport(true)
}

func (m *tuiModel) appendOutput(text string) {
	m.ensureOutputCursor()
	for len(text) > 0 {
		switch text[0] {
		case '\x1b':
			if consumed, final, params, isCSI := parseTerminalEscape(text); consumed > 0 {
				if isCSI {
					m.applyCSI(final, params)
				}
				text = text[consumed:]
				continue
			}
		case '\r':
			m.outputCol = 0
			text = text[1:]
			continue
		case '\n':
			m.newOutputLine()
			text = text[1:]
			continue
		case '\b', '\x7f':
			if m.outputCol > 0 {
				m.outputCol--
			}
			text = text[1:]
			continue
		case '\t':
			spaces := 4 - (m.outputCol % 4)
			for range spaces {
				m.writeOutputRune(' ')
			}
			text = text[1:]
			continue
		}

		r, size := utf8.DecodeRuneInString(text)
		if r == utf8.RuneError && size == 1 {
			text = text[size:]
			continue
		}
		text = text[size:]
		if r >= 0x20 && r != 0x7f {
			m.writeOutputRune(r)
		}
	}

	if len(m.output) > 2000 {
		trimmed := len(m.output) - 2000
		m.output = m.output[trimmed:]
		m.outputRow -= trimmed
		if m.outputRow < 0 {
			m.outputRow = 0
		}
	}
	m.syncOutputViewport(false)
}

func (m *tuiModel) ensureOutputCursor() {
	if len(m.output) == 0 {
		m.setOutput("")
		return
	}
	if m.outputRow < 0 || m.outputRow >= len(m.output) {
		m.outputRow = len(m.output) - 1
		m.outputCol = runeLen(m.output[m.outputRow])
		return
	}
	if m.outputCol < 0 {
		m.outputCol = 0
	}
	lineLen := runeLen(m.output[m.outputRow])
	if m.outputCol > lineLen {
		m.outputCol = lineLen
	}
}

func (m *tuiModel) newOutputLine() {
	m.ensureOutputCursor()
	if m.outputRow >= len(m.output)-1 {
		m.output = append(m.output, "")
		m.outputRow = len(m.output) - 1
	} else {
		m.outputRow++
	}
	m.outputCol = 0
}

func (m *tuiModel) writeOutputRune(r rune) {
	m.ensureOutputCursor()
	line := []rune(m.output[m.outputRow])
	for len(line) < m.outputCol {
		line = append(line, ' ')
	}
	if m.outputCol == len(line) {
		line = append(line, r)
	} else {
		line[m.outputCol] = r
	}
	m.output[m.outputRow] = string(line)
	m.outputCol++
}

func (m *tuiModel) applyCSI(final byte, params []int) {
	switch final {
	case 'A':
		m.outputRow = maxInt(0, m.outputRow-csiParam(params, 0, 1))
	case 'B':
		m.outputRow += csiParam(params, 0, 1)
		m.ensureOutputRows(m.outputRow)
	case 'C':
		m.outputCol += csiParam(params, 0, 1)
	case 'D':
		m.outputCol = maxInt(0, m.outputCol-csiParam(params, 0, 1))
	case 'G':
		m.outputCol = maxInt(0, csiParam(params, 0, 1)-1)
	case 'H', 'f':
		row := maxInt(0, csiParam(params, 0, 1)-1)
		col := maxInt(0, csiParam(params, 1, 1)-1)
		m.ensureOutputRows(row)
		m.outputRow = row
		m.outputCol = col
	case 'J':
		switch csiParam(params, 0, 0) {
		case 2, 3:
			m.setOutput("")
		default:
			m.eraseLineFromCursor()
			if m.outputRow+1 < len(m.output) {
				m.output = m.output[:m.outputRow+1]
			}
		}
	case 'K':
		switch csiParam(params, 0, 0) {
		case 1:
			m.eraseLineToCursor()
		case 2:
			m.output[m.outputRow] = ""
			m.outputCol = 0
		default:
			m.eraseLineFromCursor()
		}
	}
	m.ensureOutputCursor()
}

func (m *tuiModel) ensureOutputRows(row int) {
	for len(m.output) <= row {
		m.output = append(m.output, "")
	}
}

func (m *tuiModel) eraseLineFromCursor() {
	m.ensureOutputCursor()
	line := []rune(m.output[m.outputRow])
	if m.outputCol < len(line) {
		m.output[m.outputRow] = string(line[:m.outputCol])
	}
}

func (m *tuiModel) eraseLineToCursor() {
	m.ensureOutputCursor()
	line := []rune(m.output[m.outputRow])
	limit := minInt(m.outputCol+1, len(line))
	for i := 0; i < limit; i++ {
		line[i] = ' '
	}
	m.output[m.outputRow] = string(line)
}

func parseTerminalEscape(text string) (int, byte, []int, bool) {
	if len(text) < 2 || text[0] != '\x1b' {
		return 0, 0, nil, false
	}

	switch text[1] {
	case '[':
		for i := 2; i < len(text); i++ {
			if text[i] >= 0x40 && text[i] <= 0x7e {
				return i + 1, text[i], parseCSIParams(text[2:i]), true
			}
		}
		return len(text), 0, nil, false
	case ']':
		for i := 2; i < len(text); i++ {
			if text[i] == '\a' {
				return i + 1, 0, nil, false
			}
			if text[i] == '\x1b' && i+1 < len(text) && text[i+1] == '\\' {
				return i + 2, 0, nil, false
			}
		}
		return len(text), 0, nil, false
	default:
		return 2, 0, nil, false
	}
}

func parseCSIParams(raw string) []int {
	raw = strings.TrimLeft(raw, "?<>= ")
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ";")
	params := make([]int, 0, len(parts))
	for _, part := range parts {
		value := 0
		for _, r := range part {
			if r < '0' || r > '9' {
				break
			}
			value = value*10 + int(r-'0')
		}
		params = append(params, value)
	}
	return params
}

func csiParam(params []int, index int, fallback int) int {
	if index >= len(params) || params[index] == 0 {
		return fallback
	}
	return params[index]
}

func panelContentHeight(panelHeight int) int {
	return maxInt(1, panelHeight-3)
}

func panelInnerWidth(panelWidth int) int {
	return maxInt(12, panelWidth-2)
}

func formatDuration(duration time.Duration) string {
	if duration < time.Minute {
		return "less than a minute"
	}
	if duration < time.Hour {
		return fmt.Sprintf("%dm", int(duration.Minutes()))
	}
	if duration < 24*time.Hour {
		return fmt.Sprintf("%dh", int(duration.Hours()))
	}
	return fmt.Sprintf("%dd", int(duration.Hours()/24))
}

func runeLen(value string) int {
	return utf8.RuneCountInString(value)
}

func (m tuiModel) leftWidth(total int) int {
	width := total * 36 / 100
	width = maxInt(width, 34)
	width = minInt(width, 58)
	if total < 100 {
		width = maxInt(32, total/2)
	}
	return width
}

func (m tuiModel) outputCols() int {
	width := maxInt(m.width, 80)
	leftWidth := m.leftWidth(width)
	rightWidth := maxInt(30, width-leftWidth)
	return maxInt(20, panelInnerWidth(rightWidth))
}

func (m tuiModel) outputRows() int {
	bodyHeight := maxInt(12, maxInt(m.height, 24)-1)
	return maxInt(8, panelContentHeight(bodyHeight)-1)
}

func (m *tuiModel) resizeTaskPTY() {
	if m.task == nil || m.task.pty == nil {
		return
	}
	_ = pty.Setsize(m.task.pty, &pty.Winsize{
		Cols: uint16(m.outputCols()),
		Rows: uint16(m.outputRows()),
	})
}

func (m *tuiModel) stopTask() {
	if m.task == nil {
		return
	}
	if m.task.pty != nil {
		_ = m.task.pty.Close()
	}
	if m.task.cmd != nil && m.task.cmd.Process != nil {
		_ = m.task.cmd.Process.Kill()
	}
	m.task = nil
}

// loadTUIState refreshes the full context (auth, projects, clusters, pods) on a
// background goroutine, streaming a progress update before each step so the
// splash / status line reflects what is currently running. The final event on
// the channel is the assembled tuiStateMsg.
func loadTUIState() tea.Cmd {
	return func() tea.Msg {
		ch := make(chan tea.Msg, 8)
		go runContextLoad(ch)
		return loadStartedMsg{ch: ch}
	}
}

func runContextLoad(ch chan tea.Msg) {
	ch <- loadProgressMsg{ch: ch, status: "Checking authentication"}
	msg := tuiStateMsg{
		authenticated:  isAuthenticated(),
		currentProject: getCurrentProject(),
		currentCluster: getCurrentKubectlCluster(),
	}

	if !msg.authenticated {
		ch <- msg
		return
	}

	// Projects and clusters are independent: the cluster list only needs the
	// already-known current project, not the project list. Fetch them
	// concurrently so the slower of the two bounds the wait instead of the sum.
	loadClusters := msg.currentProject != ""
	if loadClusters {
		ch <- loadProgressMsg{ch: ch, status: fmt.Sprintf("Loading projects and clusters in %s", msg.currentProject)}
	} else {
		ch <- loadProgressMsg{ch: ch, status: "Loading projects"}
	}

	var (
		wg          sync.WaitGroup
		projects    []GCPProject
		projectsErr error
		clusters    []internal.ClusterInfo
		clustersErr error
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		projects, projectsErr = getGCPProjects()
	}()

	if loadClusters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			clusters, clustersErr = internal.GetGKEClusters(msg.currentProject)
		}()
	}

	wg.Wait()

	if projectsErr != nil {
		msg.warnings = append(msg.warnings, fmt.Sprintf("projects unavailable: %v", projectsErr))
	} else {
		msg.projects = projects
		msg.projectsLoaded = true
	}

	if !loadClusters {
		// No current project selected, so there are no clusters or pods to load.
		ch <- msg
		return
	}

	if clustersErr != nil {
		msg.warnings = append(msg.warnings, fmt.Sprintf("clusters unavailable: %v", clustersErr))
	} else {
		msg.clusters = clusters
		msg.clustersLoaded = true
	}

	if msg.currentCluster != "" {
		ch <- loadProgressMsg{ch: ch, status: "Loading pods"}
		pods, err := internal.GetDetailedPodInfo()
		if err != nil {
			msg.warnings = append(msg.warnings, fmt.Sprintf("pods unavailable: %v", err))
		} else {
			msg.pods = pods
			msg.podsLoaded = true
		}
	}

	ch <- msg
}

// waitForLoadEvent blocks for the next streamed load event (progress or the
// final tuiStateMsg) and hands it back to the update loop.
func waitForLoadEvent(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

func loadTUIStateCache() (tuiStateCache, bool) {
	path, err := tuiCachePath()
	if err != nil {
		return tuiStateCache{}, false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return tuiStateCache{}, false
	}

	var cache tuiStateCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return tuiStateCache{}, false
	}
	if cache.CachedAt.IsZero() {
		return tuiStateCache{}, false
	}

	return cache, true
}

func cacheHasUsefulData(cache tuiStateCache) bool {
	return cache.Authenticated ||
		strings.TrimSpace(cache.CurrentProject) != "" ||
		strings.TrimSpace(cache.CurrentCluster) != "" ||
		strings.TrimSpace(cache.SelectedPod) != "" ||
		cacheHasPaneData(cache)
}

func cacheHasPaneData(cache tuiStateCache) bool {
	return len(cache.Projects) > 0 || len(cache.Clusters) > 0 || len(cache.Pods) > 0
}

func saveTUIStateCache(cache tuiStateCache) error {
	path, err := tuiCachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

func loadTUIPreferences() (tuiPreferences, bool) {
	path, err := tuiPreferencesPath()
	if err != nil {
		return tuiPreferences{}, false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return tuiPreferences{}, false
	}

	var preferences tuiPreferences
	if err := json.Unmarshal(data, &preferences); err != nil {
		return tuiPreferences{}, false
	}
	return preferences, true
}

func saveTUIPreferences(preferences tuiPreferences) error {
	path, err := tuiPreferencesPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(preferences, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

func clearTUIPreferences() error {
	path, err := tuiPreferencesPath()
	if err != nil {
		return err
	}

	err = os.Remove(path)
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return err
}

func tuiCachePath() (string, error) {
	return tuiConfigPath("tui-state.json")
}

func tuiPreferencesPath() (string, error) {
	return tuiConfigPath("tui-preferences.json")
}

func tuiConfigPath(filename string) (string, error) {
	if dir := strings.TrimSpace(os.Getenv("GCPEASY_CONFIG_DIR")); dir != "" {
		return filepath.Join(dir, filename), nil
	}

	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); dir != "" {
		return filepath.Join(dir, "gcpeasy", filename), nil
	}

	dir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".config", "gcpeasy", filename), nil
}

func switchProject(project GCPProject) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("gcloud", "config", "set", "project", project.ProjectID)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return tuiOperationMsg{
				label:  "Switch project",
				output: string(output),
				err:    commandError(err, output),
			}
		}
		return tuiOperationMsg{label: "Switch project", output: string(output), refresh: true}
	}
}

func switchCluster(projectID string, cluster internal.ClusterInfo) tea.Cmd {
	return func() tea.Msg {
		args := []string{
			"container",
			"clusters",
			"get-credentials",
			cluster.Name,
			"--location",
			cluster.Location,
			"--project",
			projectID,
		}
		cmd := exec.Command("gcloud", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return tuiOperationMsg{
				label:  "Switch cluster",
				output: string(output),
				err:    commandError(err, output),
			}
		}
		return tuiOperationMsg{label: "Switch cluster", output: string(output), refresh: true}
	}
}

func logout() tea.Cmd {
	return func() tea.Msg {
		accountCmd := exec.Command("gcloud", "auth", "list", "--filter=status:ACTIVE", "--format=value(account)")
		output, err := accountCmd.Output()
		if err != nil {
			return tuiOperationMsg{label: "Logout", err: err}
		}

		account := strings.TrimSpace(string(output))
		if account == "" {
			return tuiOperationMsg{label: "Logout", output: "No active account found.", refresh: true}
		}

		revokeCmd := exec.Command("gcloud", "auth", "revoke", account)
		revokeOutput, err := revokeCmd.CombinedOutput()
		if err != nil {
			return tuiOperationMsg{label: "Logout", output: string(revokeOutput), err: commandError(err, revokeOutput)}
		}
		return tuiOperationMsg{label: "Logout", output: string(revokeOutput), refresh: true}
	}
}

func startTask(spec taskSpec, cols int, rows int) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command(spec.name, spec.args...)
		cmd.Env = append(os.Environ(), "TERM=dumb", "NO_COLOR=1", "CLICOLOR=0")
		ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
			Cols: uint16(maxInt(20, cols)),
			Rows: uint16(maxInt(8, rows)),
		})
		if err != nil {
			return taskStartedMsg{spec: spec, err: err}
		}

		out := make(chan string, 64)
		done := make(chan error, 1)
		task := &runningTask{
			spec: spec,
			cmd:  cmd,
			pty:  ptmx,
			out:  out,
			done: done,
		}

		go func() {
			defer close(out)
			buffer := make([]byte, 4096)
			for {
				n, err := ptmx.Read(buffer)
				if n > 0 {
					out <- string(buffer[:n])
				}
				if err != nil {
					return
				}
			}
		}()

		go func() {
			done <- cmd.Wait()
			close(done)
		}()

		return taskStartedMsg{spec: spec, task: task}
	}
}

func waitForTaskOutput(task *runningTask) tea.Cmd {
	return func() tea.Msg {
		text, ok := <-task.out
		return taskOutputMsg{text: text, ok: ok}
	}
}

func waitForTaskDone(task *runningTask) tea.Cmd {
	return func() tea.Msg {
		err, ok := <-task.done
		if !ok {
			err = nil
		}
		return taskDoneMsg{spec: task.spec, err: err}
	}
}

func sendTaskInput(input string) tea.Cmd {
	return func() tea.Msg {
		return taskInputMsg(input)
	}
}

type taskInputMsg string

func shellTask(title string, script string, refresh bool, interactive bool) taskSpec {
	return taskSpec{
		title:       title,
		name:        "sh",
		args:        []string{"-lc", script},
		refresh:     refresh,
		interactive: interactive,
	}
}

func keyInput(msg tea.KeyMsg) (string, bool) {
	if len(msg.Runes) > 0 {
		return string(msg.Runes), true
	}

	switch msg.String() {
	case "esc", "ctrl+[":
		return "\x1b", true
	case "enter":
		return "\r", true
	case "tab":
		return "\t", true
	case "backspace":
		return "\x7f", true
	case "delete":
		return "\x1b[3~", true
	case "up":
		return "\x1b[A", true
	case "down":
		return "\x1b[B", true
	case "right":
		return "\x1b[C", true
	case "left":
		return "\x1b[D", true
	case "home":
		return "\x1b[H", true
	case "end":
		return "\x1b[F", true
	case "ctrl+d":
		return "\x04", true
	case "ctrl+l":
		return "\x0c", true
	}

	return "", false
}

func commandError(err error, output []byte) error {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, text)
}

func commandLine(name string, args []string) string {
	return "$ " + shellCommand(name, args)
}

func shellCommand(name string, args []string) string {
	parts := append([]string{name}, args...)
	for i, part := range parts {
		if strings.ContainsAny(part, " \t\n'\"") {
			parts[i] = shQuote(part)
		}
	}
	return strings.Join(parts, " ")
}

func shQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"$`\\|&;<>()") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func podRef(pod internal.PodInfo) string {
	return pod.Namespace + "/" + pod.Name
}

func visibleRows(rows []string, cursor int, height int) []string {
	if len(rows) == 0 {
		return rows
	}

	visible := maxInt(1, height)
	if len(rows) <= visible {
		return rows
	}

	start := cursor - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > len(rows) {
		start = len(rows) - visible
	}

	return rows[start : start+visible]
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func fallback(value string, fallbackValue string) string {
	if strings.TrimSpace(value) == "" {
		return fallbackValue
	}
	return value
}

func shortContext(context string) string {
	if context == "" {
		return ""
	}
	parts := strings.Split(context, "_")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return context
}

func clip(value string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= maxWidth {
		return value
	}
	if maxWidth <= 3 {
		return value[:maxWidth]
	}

	runes := []rune(value)
	for len(runes) > 0 && lipgloss.Width(string(runes))+3 > maxWidth {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "..."
}

func hiddenSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = true
		}
	}
	return set
}

func hiddenKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key, hidden := range set {
		if hidden {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

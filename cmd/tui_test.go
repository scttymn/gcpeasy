package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	lipgloss "charm.land/lipgloss/v2"
	tea "github.com/charmbracelet/bubbletea"

	"gcpeasy/internal"
)

func newTestTUIModel(t *testing.T) tuiModel {
	t.Helper()
	t.Setenv("GCPEASY_CONFIG_DIR", t.TempDir())
	return newTUIModel()
}

func TestUnauthenticatedStateShowsAuthDialog(t *testing.T) {
	model := newTestTUIModel(t)

	updated, _ := model.Update(tuiStateMsg{authenticated: false})
	got := updated.(tuiModel)

	if !got.authDialog {
		t.Fatal("expected auth dialog to be shown when user is unauthenticated")
	}
	if got.status != "Authentication required" {
		t.Fatalf("expected authentication status, got %q", got.status)
	}
}

func TestStartupRendersBootScreenBeforeAuthCheck(t *testing.T) {
	model := newTestTUIModel(t)
	model.width = 120
	model.height = 32

	view := stripANSI(model.View())
	for _, want := range []string{"GCPEASY", "checking authentication", "Loading your GCP workspace"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected boot screen to include %q, got %q", want, view)
		}
	}
	if strings.Contains(view, "[1]-Environments") || strings.Contains(view, "Command palette") {
		t.Fatalf("expected boot screen to hide main UI, got %q", view)
	}
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func stripANSI(value string) string {
	return ansiPattern.ReplaceAllString(value, "")
}

func TestPodFooterHintsExposePodActions(t *testing.T) {
	model := newTestTUIModel(t)
	model.authenticated = true
	model.focus = panelPods
	model.pods = []internal.PodInfo{
		{
			Namespace: "app",
			Name:      "web-123",
			Status:    "Running",
			Ready:     "1/1",
			Age:       "3m",
		},
	}

	hints := strings.Join(model.footerHints(), " | ")
	for _, want := range []string{"Select: enter", "Logs: l", "Follow: f", "Shell: s", "Console: c", "Describe: d"} {
		if !strings.Contains(hints, want) {
			t.Fatalf("expected pod hints to include %q, got %q", want, hints)
		}
	}
}

func TestPodActionsRefreshClusterCredentials(t *testing.T) {
	model := newTestTUIModel(t)
	model.authenticated = true
	model.focus = panelPods
	model.currentProject = "project-dev"
	model.currentCluster = "gke_project-dev_us-central1_cluster-dev"
	model.clusters = []internal.ClusterInfo{{Name: "cluster-dev", Location: "us-central1"}}
	model.pods = []internal.PodInfo{{Namespace: "app", Name: "web-123", Status: "Running"}}

	updated, cmd := model.runPodLogs(false)
	got := updated.(tuiModel)
	output := strings.Join(got.output, "\n")

	if cmd == nil {
		t.Fatal("expected pod logs to start a task")
	}
	for _, want := range []string{
		"gcloud container clusters get-credentials cluster-dev --location us-central1 --project project-dev",
		"kubectl logs web-123 -n app",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected pod action output to include %q, got %q", want, output)
		}
	}
}

func TestKubectlScriptFallsBackWhenClusterUnknown(t *testing.T) {
	model := newTestTUIModel(t)

	script := model.kubectlScript([]string{"logs", "web-123", "-n", "app"})

	if strings.Contains(script, "get-credentials") {
		t.Fatalf("expected kubectl script without cluster metadata not to refresh credentials, got %q", script)
	}
	if script != "kubectl logs web-123 -n app" {
		t.Fatalf("expected direct kubectl fallback, got %q", script)
	}
}

func TestEnvironmentFooterHintShowsAlreadyActive(t *testing.T) {
	model := newTestTUIModel(t)
	model.authenticated = true
	model.focus = panelEnvironments
	model.currentProject = "project-dev"
	model.projects = []GCPProject{
		{ProjectID: "project-dev", Name: "Development"},
	}

	hints := strings.Join(model.footerHints(), " | ")
	if !strings.Contains(hints, "Already active: enter") {
		t.Fatalf("expected active environment hint, got %q", hints)
	}
}

func TestFooterAdvertisesSpaceCommandsAndHelp(t *testing.T) {
	model := newTestTUIModel(t)
	model.authenticated = true
	model.focus = panelEnvironments

	hints := strings.Join(model.footerHints(), " | ")
	for _, want := range []string{"Commands: <space>", "Help: ?"} {
		if !strings.Contains(hints, want) {
			t.Fatalf("expected footer hints to include %q, got %q", want, hints)
		}
	}
	for _, stale := range []string{"4/p commands", "space commands", "? help", "tab/1-3 focus", "0 output"} {
		if strings.Contains(hints, stale) {
			t.Fatalf("expected footer hints to hide %q, got %q", stale, hints)
		}
	}
}

func TestAppendOutputSanitizesTerminalControlSequences(t *testing.T) {
	model := newTestTUIModel(t)
	model.setOutput("")

	model.appendOutput("\x1b[2J\x1b[H[1] pry(main)> \x1b[32mok\x1b[0m\r\nnext\x07\x1b[?25l")
	got := strings.Join(model.output, "\n")

	for _, unsafe := range []string{"\x1b", "\x07", "\r"} {
		if strings.Contains(got, unsafe) {
			t.Fatalf("expected output to remove %q from %q", unsafe, got)
		}
	}
	if !strings.Contains(got, "[1] pry(main)> ok") {
		t.Fatalf("expected sanitized Pry prompt output, got %q", got)
	}
	if !strings.Contains(got, "next") {
		t.Fatalf("expected sanitized following output, got %q", got)
	}
}

func TestAppendOutputRepaintsCurrentLine(t *testing.T) {
	model := newTestTUIModel(t)
	model.setOutput("")

	model.appendOutput("[1] pry(main)> ")
	model.appendOutput("\r\x1b[K[1] pry(main)> U")
	model.appendOutput("\r\x1b[K[1] pry(main)> Us")
	model.appendOutput("\r\x1b[K[1] pry(main)> Use")
	model.appendOutput("\r\x1b[K[1] pry(main)> User.first")

	if len(model.output) != 1 {
		t.Fatalf("expected repaint to stay on one line, got %d lines: %#v", len(model.output), model.output)
	}
	if model.output[0] != "[1] pry(main)> User.first" {
		t.Fatalf("expected final repaint line, got %q", model.output[0])
	}
}

func TestInteractiveEscapePassesThroughToTask(t *testing.T) {
	model := newTestTUIModel(t)
	model.focus = panelOutput
	model.task = &runningTask{spec: taskSpec{interactive: true}}

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(tuiModel)

	if got.focus != panelOutput {
		t.Fatalf("expected esc to keep output focused, got %v", got.focus)
	}
	if cmd == nil {
		t.Fatal("expected esc to send input to task")
	}
	msg := cmd()
	input, ok := msg.(taskInputMsg)
	if !ok {
		t.Fatalf("expected task input message, got %T", msg)
	}
	if string(input) != "\x1b" {
		t.Fatalf("expected escape byte, got %q", string(input))
	}
}

func TestInteractiveTaskRefreshRunsInBackground(t *testing.T) {
	model := newTestTUIModel(t)
	model.task = &runningTask{spec: taskSpec{title: "Shell", interactive: true, refresh: true}}

	updated, cmd := model.Update(taskDoneMsg{
		spec: taskSpec{title: "Shell", interactive: true, refresh: true},
	})
	got := updated.(tuiModel)

	if cmd == nil {
		t.Fatal("expected background refresh command")
	}
	if !got.loading {
		t.Fatal("expected refresh loading state")
	}
	if got.refreshModal {
		t.Fatal("expected interactive task refresh to avoid blocking modal")
	}
	if got.status != "Shell complete" {
		t.Fatalf("expected task completion status during background refresh, got %q", got.status)
	}
}

func TestFooterShowsBackgroundRefreshIndicator(t *testing.T) {
	model := newTestTUIModel(t)
	model.width = 80
	model.loading = true
	model.refreshModal = false

	footer := model.renderFooter(80)
	if !strings.Contains(footer, "refreshing") {
		t.Fatalf("expected background refresh indicator, got %q", footer)
	}
	if lipgloss.Width(footer) != 80 {
		t.Fatalf("expected footer to fill line, width=%d footer=%q", lipgloss.Width(footer), footer)
	}
	if !strings.HasPrefix(footer, " ") || !strings.HasSuffix(footer, " ") {
		t.Fatalf("expected footer to keep left and right gutter, got %q", footer)
	}
}

func TestLeftAndRightPanelsMatchHeight(t *testing.T) {
	model := newTestTUIModel(t)
	model.width = 160
	model.height = 48
	model.authenticated = true
	model.currentProject = "project-dev"
	model.currentCluster = "gke_project-dev_us-central1_cluster-dev"
	model.projects = []GCPProject{{ProjectID: "project-dev", Name: "Development"}}
	model.clusters = []internal.ClusterInfo{{Name: "cluster-dev", Location: "us-central1"}}
	model.pods = []internal.PodInfo{{Namespace: "app", Name: "web-123", Status: "Running", Ready: "1/1", Age: "3m"}}

	bodyHeight := model.height - 2
	leftWidth := model.leftWidth(model.width)
	left := model.renderLeft(leftWidth, bodyHeight)
	right := model.renderOutput(model.width-leftWidth, bodyHeight)

	if lipgloss.Height(left) != lipgloss.Height(right) {
		t.Fatalf("expected panel heights to match, left=%d right=%d", lipgloss.Height(left), lipgloss.Height(right))
	}
}

func TestMainBodyUsesFullTerminalWidth(t *testing.T) {
	model := newTestTUIModel(t)
	model.width = 160
	model.height = 48

	bodyHeight := model.height - 1
	leftWidth := model.leftWidth(model.width)
	left := model.renderLeft(leftWidth, bodyHeight)
	right := model.renderOutput(model.width-leftWidth, bodyHeight)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	if lipgloss.Width(body) != model.width {
		t.Fatalf("expected body width %d, got %d", model.width, lipgloss.Width(body))
	}
}

func TestPanelsRenderAtRequestedWidth(t *testing.T) {
	model := newTestTUIModel(t)

	for _, width := range []int{34, 58, 102} {
		panel := model.renderPanel("[1]-Environments", panelEnvironments, width, 12, "project-dev")
		if got := lipgloss.Width(panel); got != width {
			t.Fatalf("expected panel width %d, got %d", width, got)
		}
	}
}

func TestSelectedRowsFillPanelInterior(t *testing.T) {
	model := newTestTUIModel(t)
	model.focus = panelEnvironments
	model.cursors[panelEnvironments] = 0

	width := 60
	row := model.renderRow(panelEnvironments, 0, " ", "project-dev", "Development", width, false)
	if lipgloss.Width(row) != panelInnerWidth(width) {
		t.Fatalf("expected selected row width %d, got %d: %q", panelInnerWidth(width), lipgloss.Width(row), row)
	}
}

func TestHiddenRowsUseHiddenStyle(t *testing.T) {
	model := newTestTUIModel(t)
	model.focus = panelEnvironments
	model.cursors[panelEnvironments] = 1

	width := 60
	normal := model.renderRow(panelEnvironments, 0, " ", "project-dev", "Development", width, false)
	hidden := model.renderRow(panelEnvironments, 0, " ", "project-dev", "Development", width, true)
	selectedHidden := model.renderRow(panelEnvironments, 1, " ", "project-prod", "Production", width, true)

	if hidden == normal {
		t.Fatal("expected hidden row to render differently from normal row")
	}
	if !strings.Contains(hidden, "\x1b[") {
		t.Fatalf("expected hidden row to include style escape sequences, got %q", hidden)
	}
	if lipgloss.Width(hidden) != lipgloss.Width(normal) {
		t.Fatalf("expected hidden row width to match normal row, hidden=%d normal=%d", lipgloss.Width(hidden), lipgloss.Width(normal))
	}
	if !strings.Contains(selectedHidden, "\x1b[") {
		t.Fatalf("expected selected hidden row to include style escape sequences, got %q", selectedHidden)
	}
	if lipgloss.Width(selectedHidden) != panelInnerWidth(width) {
		t.Fatalf("expected selected hidden row to fill panel interior, got %d", lipgloss.Width(selectedHidden))
	}
}

func TestLongPodRowsDoNotWrap(t *testing.T) {
	model := newTestTUIModel(t)
	model.authenticated = true
	model.currentCluster = "cluster-dev"
	model.focus = panelPods
	model.pods = []internal.PodInfo{
		{
			Namespace: "carebility",
			Name:      "carebility-web-app-deployment-77f7b9c5f-2rwnq",
			Status:    "Running",
			Ready:     "1/1",
			Age:       "3m",
		},
		{
			Namespace: "carebility",
			Name:      "carebility-data-pipelines-adp-worker-0123456789abcdef",
			Status:    "Running",
			Ready:     "1/1",
			Age:       "9m",
		},
	}

	width := 58
	height := 14
	panel := model.renderPodPanel(width, height)
	if got := lipgloss.Width(panel); got != width {
		t.Fatalf("expected pod panel width %d, got %d", width, got)
	}
	if got := lipgloss.Height(panel); got != height {
		t.Fatalf("expected pod panel height %d, got %d", height, got)
	}
}

func TestCommandsOpenInModal(t *testing.T) {
	model := newTestTUIModel(t)
	model.width = 140
	model.height = 36
	model.booting = false

	if strings.Contains(model.View(), "Command palette") {
		t.Fatal("expected commands to stay hidden until opened")
	}

	model.openCommandModal()
	view := model.View()
	if !strings.Contains(view, "Command palette") {
		t.Fatal("expected command palette modal in view")
	}
	if !strings.Contains(view, "Refresh context") {
		t.Fatal("expected command palette to include command actions")
	}
	if !strings.Contains(view, "Reset visibility") {
		t.Fatal("expected command palette to include reset visibility action")
	}
	if strings.Contains(view, "View pod logs") || strings.Contains(view, "Open pod shell") || strings.Contains(view, "Open Rails console") {
		t.Fatal("expected pod actions to stay out of command palette")
	}
}

func TestSpaceOnlyOpensCommandPalette(t *testing.T) {
	model := newTestTUIModel(t)

	for _, key := range []rune{'4', 'p'} {
		updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		got := updated.(tuiModel)
		if got.commandModal {
			t.Fatalf("expected %q not to open command palette", key)
		}
	}

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeySpace})
	got := updated.(tuiModel)
	if !got.commandModal {
		t.Fatal("expected space to open command palette")
	}
}

func TestQuestionMarkOpensHelpModal(t *testing.T) {
	model := newTestTUIModel(t)
	model.width = 140
	model.height = 36
	model.booting = false

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	got := updated.(tuiModel)

	if !got.helpModal {
		t.Fatal("expected ? to open help modal")
	}
	view := got.View()
	for _, want := range []string{"Help", "tab / shift+tab", "0", "space"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help modal to include %q", want)
		}
	}
	for _, want := range []string{"h", "Hide or unhide", "H", "Toggle showing hidden"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help modal to include hidden item help %q", want)
		}
	}
}

func TestHideEnvironmentFiltersAndPersistsPreference(t *testing.T) {
	model := newTestTUIModel(t)
	model.authenticated = true
	model.focus = panelEnvironments
	model.projects = []GCPProject{
		{ProjectID: "project-noise", Name: "Noise"},
		{ProjectID: "project-work", Name: "Work"},
	}

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	got := updated.(tuiModel)

	if len(got.visibleProjects()) != 1 || got.visibleProjects()[0].ProjectID != "project-work" {
		t.Fatalf("expected hidden project to be filtered, got %#v", got.visibleProjects())
	}

	preferences, ok := loadTUIPreferences()
	if !ok {
		t.Fatal("expected preferences to be saved")
	}
	if len(preferences.HiddenEnvironments) != 1 || preferences.HiddenEnvironments[0] != "project-noise" {
		t.Fatalf("expected hidden environment preference, got %#v", preferences.HiddenEnvironments)
	}

	reloaded := newTUIModel()
	reloaded.authenticated = true
	reloaded.projects = model.projects
	if len(reloaded.visibleProjects()) != 1 || reloaded.visibleProjects()[0].ProjectID != "project-work" {
		t.Fatalf("expected preferences to load outside cache, got %#v", reloaded.visibleProjects())
	}
}

func TestResetVisibilityClearsHiddenPreferences(t *testing.T) {
	model := newTestTUIModel(t)
	model.authenticated = true
	model.focus = panelEnvironments
	model.showHidden = true
	model.projects = []GCPProject{
		{ProjectID: "project-noise", Name: "Noise"},
		{ProjectID: "project-work", Name: "Work"},
	}
	model.hiddenEnvironments["project-noise"] = true
	model.hiddenClusters["project-dev\x1fus-central1\x1fcluster-dev"] = true
	model.hiddenPods["project-dev\x1fgke_project-dev_us-central1_cluster-dev\x1fapp\x1fweb-123"] = true
	model.savePreferences()

	updated, _ := model.runCommandByID("reset_visibility")
	got := updated.(tuiModel)

	if got.showHidden {
		t.Fatal("expected reset visibility to leave show hidden mode")
	}
	if len(got.hiddenEnvironments) != 0 || len(got.hiddenClusters) != 0 || len(got.hiddenPods) != 0 {
		t.Fatalf("expected hidden preferences to be cleared, got env=%v clusters=%v pods=%v", got.hiddenEnvironments, got.hiddenClusters, got.hiddenPods)
	}
	if len(got.visibleProjects()) != 2 {
		t.Fatalf("expected all projects to be visible, got %#v", got.visibleProjects())
	}

	path, err := tuiPreferencesPath()
	if err != nil {
		t.Fatalf("expected preferences path: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected preferences file to be removed, stat err=%v", err)
	}
}

func TestShowHiddenAllowsUnhidingSelectedItem(t *testing.T) {
	model := newTestTUIModel(t)
	model.authenticated = true
	model.focus = panelPods
	model.currentProject = "project-dev"
	model.currentCluster = "gke_project-dev_us-central1_cluster-dev"
	model.pods = []internal.PodInfo{
		{Namespace: "app", Name: "web-123"},
		{Namespace: "app", Name: "worker-456"},
	}

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	hidden := updated.(tuiModel)
	if len(hidden.visiblePods()) != 1 || hidden.visiblePods()[0].Name != "worker-456" {
		t.Fatalf("expected hidden pod to be filtered, got %#v", hidden.visiblePods())
	}

	updated, _ = hidden.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}})
	showing := updated.(tuiModel)
	if !showing.showHidden {
		t.Fatal("expected H to show hidden items")
	}
	if len(showing.visiblePods()) != 2 {
		t.Fatalf("expected all pods while showing hidden, got %#v", showing.visiblePods())
	}

	updated, _ = showing.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	unhidden := updated.(tuiModel)
	if unhidden.isPodHidden(model.pods[0]) {
		t.Fatal("expected h to unhide selected hidden pod while showing hidden items")
	}
}

func TestStartAuthenticationUsesExternalTerminalFlow(t *testing.T) {
	model := newTestTUIModel(t)
	model.authDialog = true

	updated, cmd := model.startAuthentication()
	got := updated.(tuiModel)

	if got.authDialog {
		t.Fatal("expected auth dialog to close while authentication runs")
	}
	if got.focus != panelOutput {
		t.Fatalf("expected output panel focus, got %v", got.focus)
	}
	if cmd == nil {
		t.Fatal("expected authentication command")
	}
	if !strings.Contains(strings.Join(got.output, "\n"), "running in the terminal") {
		t.Fatalf("expected external auth flow message, got %q", got.output)
	}
}

func TestModelDefersCachedLeftPaneStateUntilAuthVerified(t *testing.T) {
	t.Setenv("GCPEASY_CONFIG_DIR", t.TempDir())

	cache := tuiStateCache{
		Authenticated:  true,
		CurrentProject: "project-dev",
		CurrentCluster: "gke_project-dev_us-central1_cluster-dev",
		SelectedPod:    "app/web-123",
		Projects:       []GCPProject{{ProjectID: "project-dev", Name: "Development"}},
		Clusters:       []internal.ClusterInfo{{Name: "cluster-dev", Location: "us-central1"}},
		Pods:           []internal.PodInfo{{Namespace: "app", Name: "web-123", Status: "Running"}},
		CachedAt:       time.Now().Add(-2 * time.Minute),
	}
	if err := saveTUIStateCache(cache); err != nil {
		t.Fatalf("failed to save cache: %v", err)
	}

	model := newTUIModel()

	if model.cacheLoaded {
		t.Fatal("expected startup to defer cached state until auth is verified")
	}
	if model.authenticated {
		t.Fatal("expected startup not to trust cached auth state")
	}
	if !model.booting {
		t.Fatal("expected startup to render boot screen while auth is unknown")
	}
	if len(model.projects) != 0 || len(model.clusters) != 0 || len(model.pods) != 0 {
		t.Fatalf("expected cached panes to stay hidden before auth verification, got projects=%d clusters=%d pods=%d", len(model.projects), len(model.clusters), len(model.pods))
	}
	if model.refreshModal {
		t.Fatal("expected startup to use boot screen instead of refresh modal")
	}

	updated, cmd := model.Update(tuiAuthStateMsg{
		authenticated:  true,
		currentProject: cache.CurrentProject,
		currentCluster: cache.CurrentCluster,
	})
	got := updated.(tuiModel)

	if cmd == nil {
		t.Fatal("expected auth verification to start full context refresh")
	}
	if got.booting {
		t.Fatal("expected auth verification to leave boot screen")
	}
	if !got.cacheLoaded {
		t.Fatal("expected model to load cached state after auth is verified")
	}
	if !got.authenticated {
		t.Fatal("expected verified auth state")
	}
	if got.currentProject != cache.CurrentProject {
		t.Fatalf("expected cached project %q, got %q", cache.CurrentProject, got.currentProject)
	}
	if len(got.projects) != 1 || len(got.clusters) != 1 || len(got.pods) != 1 {
		t.Fatalf("expected cached panes after auth verification, got projects=%d clusters=%d pods=%d", len(got.projects), len(got.clusters), len(got.pods))
	}
	if !got.cacheHasPanes {
		t.Fatal("expected cache pane marker")
	}
	if got.refreshModal {
		t.Fatal("expected refresh modal to close after auth verification")
	}
	if !got.loading {
		t.Fatal("expected full context refresh to continue in the background")
	}
}

func TestUnauthenticatedRefreshHidesCacheButKeepsItForLogin(t *testing.T) {
	t.Setenv("GCPEASY_CONFIG_DIR", t.TempDir())

	cache := tuiStateCache{
		Authenticated:  true,
		CurrentProject: "project-dev",
		CurrentCluster: "gke_project-dev_us-central1_cluster-dev",
		SelectedPod:    "app/web-123",
		Projects:       []GCPProject{{ProjectID: "project-dev", Name: "Development"}},
		Clusters:       []internal.ClusterInfo{{Name: "cluster-dev", Location: "us-central1"}},
		Pods:           []internal.PodInfo{{Namespace: "app", Name: "web-123", Status: "Running"}},
		CachedAt:       time.Now().Add(-2 * time.Minute),
	}
	if err := saveTUIStateCache(cache); err != nil {
		t.Fatalf("failed to save cache: %v", err)
	}

	model := newTUIModel()
	updated, _ := model.Update(tuiAuthStateMsg{authenticated: false})
	got := updated.(tuiModel)

	if !got.authDialog {
		t.Fatal("expected auth dialog when refresh reports unauthenticated")
	}
	if got.booting {
		t.Fatal("expected unauthenticated auth check to leave boot screen")
	}
	if model.cacheLoaded {
		t.Fatal("expected startup model not to expose cache")
	}
	if got.cacheLoaded || got.currentProject != "" || got.currentCluster != "" || got.selectedPod != "" {
		t.Fatalf("expected unauthenticated refresh to clear cached context, got project=%q cluster=%q pod=%q cacheLoaded=%v", got.currentProject, got.currentCluster, got.selectedPod, got.cacheLoaded)
	}
	if len(got.projects) != 0 || len(got.clusters) != 0 || len(got.pods) != 0 {
		t.Fatalf("expected unauthenticated refresh to hide cached panes, got projects=%d clusters=%d pods=%d", len(got.projects), len(got.clusters), len(got.pods))
	}
	if got.pendingCache == nil {
		t.Fatal("expected unauthenticated refresh to keep pending cache for a later login")
	}

	updated, cmd := got.Update(authDoneMsg{})
	got = updated.(tuiModel)
	if cmd == nil {
		t.Fatal("expected login completion to start background refresh")
	}
	if !got.cacheLoaded {
		t.Fatal("expected login completion to apply pending cache immediately")
	}
	if got.currentProject != cache.CurrentProject {
		t.Fatalf("expected cached project after login, got %q", got.currentProject)
	}
	if len(got.projects) != 1 || len(got.clusters) != 1 || len(got.pods) != 1 {
		t.Fatalf("expected cached panes after login, got projects=%d clusters=%d pods=%d", len(got.projects), len(got.clusters), len(got.pods))
	}
	if got.refreshModal {
		t.Fatal("expected cached login path to avoid blocking refresh modal")
	}
}

func TestModelAppliesCachedStateWithoutPaneRowsAfterAuthVerified(t *testing.T) {
	t.Setenv("GCPEASY_CONFIG_DIR", t.TempDir())

	cache := tuiStateCache{
		Authenticated:  true,
		CurrentProject: "project-dev",
		CurrentCluster: "gke_project-dev_us-central1_cluster-dev",
		CachedAt:       time.Now().Add(-2 * time.Minute),
	}
	if err := saveTUIStateCache(cache); err != nil {
		t.Fatalf("failed to save cache: %v", err)
	}

	model := newTUIModel()
	updated, _ := model.Update(tuiStateMsg{
		authenticated:  true,
		currentProject: cache.CurrentProject,
		currentCluster: cache.CurrentCluster,
	})
	got := updated.(tuiModel)

	if !got.cacheLoaded {
		t.Fatal("expected model to load cached state")
	}
	if got.cacheHasPanes {
		t.Fatal("expected cache without pane rows not to claim cached panes")
	}
	if got.currentProject != cache.CurrentProject {
		t.Fatalf("expected cached project %q, got %q", cache.CurrentProject, got.currentProject)
	}
	if got.refreshModal {
		t.Fatal("expected refresh modal to close after auth verification")
	}
}

func TestStateRefreshPreservesCachedPanesOnPartialFailure(t *testing.T) {
	model := newTestTUIModel(t)
	model.authenticated = true
	model.projects = []GCPProject{{ProjectID: "cached-project", Name: "Cached"}}
	model.clusters = []internal.ClusterInfo{{Name: "cached-cluster", Location: "us-central1"}}
	model.pods = []internal.PodInfo{{Namespace: "app", Name: "cached-pod"}}

	updated, _ := model.Update(tuiStateMsg{
		authenticated:  true,
		currentProject: "fresh-project",
		currentCluster: "fresh-cluster",
		warnings:       []string{"clusters unavailable"},
	})
	got := updated.(tuiModel)

	if got.currentProject != "fresh-project" {
		t.Fatalf("expected current project to update, got %q", got.currentProject)
	}
	if got.projects[0].ProjectID != "cached-project" {
		t.Fatalf("expected cached projects to remain, got %#v", got.projects)
	}
	if got.clusters[0].Name != "cached-cluster" {
		t.Fatalf("expected cached clusters to remain, got %#v", got.clusters)
	}
	if got.pods[0].Name != "cached-pod" {
		t.Fatalf("expected cached pods to remain, got %#v", got.pods)
	}
	if got.refreshModal {
		t.Fatal("expected refresh modal to close after refresh result")
	}
}

func TestExpiredSessionPreservesVisiblePanesForReauthentication(t *testing.T) {
	model := newTestTUIModel(t)
	model.authenticated = true
	model.currentProject = "project-dev"
	model.currentCluster = "gke_project-dev_us-central1_cluster-dev"
	model.selectedPod = "app/web-123"
	model.projects = []GCPProject{{ProjectID: "project-dev", Name: "Development"}}
	model.clusters = []internal.ClusterInfo{{Name: "cluster-dev", Location: "us-central1"}}
	model.pods = []internal.PodInfo{{Namespace: "app", Name: "web-123", Status: "Running"}}

	updated, _ := model.Update(tuiStateMsg{authenticated: false})
	loggedOut := updated.(tuiModel)

	if !loggedOut.authDialog {
		t.Fatal("expected auth dialog after session expires")
	}
	if len(loggedOut.projects) != 0 || len(loggedOut.clusters) != 0 || len(loggedOut.pods) != 0 {
		t.Fatalf("expected expired session to hide panes, got projects=%d clusters=%d pods=%d", len(loggedOut.projects), len(loggedOut.clusters), len(loggedOut.pods))
	}
	if loggedOut.pendingCache == nil {
		t.Fatal("expected expired session to keep visible panes as pending cache")
	}

	updated, _ = loggedOut.Update(authDoneMsg{})
	reauthed := updated.(tuiModel)

	if !reauthed.cacheLoaded {
		t.Fatal("expected reauthentication to restore cached panes immediately")
	}
	if reauthed.currentProject != "project-dev" {
		t.Fatalf("expected cached project after reauthentication, got %q", reauthed.currentProject)
	}
	if len(reauthed.projects) != 1 || len(reauthed.clusters) != 1 || len(reauthed.pods) != 1 {
		t.Fatalf("expected cached panes after reauthentication, got projects=%d clusters=%d pods=%d", len(reauthed.projects), len(reauthed.clusters), len(reauthed.pods))
	}
	if reauthed.refreshModal {
		t.Fatal("expected reauthentication with cache to refresh in the background")
	}
}

func TestRefreshModalRendersOverContent(t *testing.T) {
	model := newTestTUIModel(t)
	model.width = 120
	model.height = 32
	model.booting = false
	model.refreshModal = true

	view := model.View()
	if !strings.Contains(view, "Refreshing") {
		t.Fatalf("expected refresh modal in view")
	}
	if !strings.Contains(view, "Loading environments, clusters, and pods") {
		t.Fatalf("expected refresh modal detail in view")
	}
	if strings.Contains(view, "│     │") {
		t.Fatalf("expected modal rendering not to leave broken border fragments")
	}
}

func TestManualRefreshOutputClearsWhenRefreshCompletes(t *testing.T) {
	model := newTestTUIModel(t)
	model.refreshOutput = true
	model.setOutput("Refreshing context...")

	updated, _ := model.Update(tuiStateMsg{
		authenticated:  true,
		currentProject: "project-dev",
		currentCluster: "cluster-dev",
		projects:       []GCPProject{{ProjectID: "project-dev"}},
		clusters:       []internal.ClusterInfo{{Name: "cluster-dev"}},
		pods:           []internal.PodInfo{{Namespace: "app", Name: "web-123"}},
		projectsLoaded: true,
		clustersLoaded: true,
		podsLoaded:     true,
	})
	got := updated.(tuiModel)
	output := strings.Join(got.output, "\n")

	if strings.Contains(output, "Refreshing context...") {
		t.Fatalf("expected refresh output to be replaced, got %q", output)
	}
	if !strings.Contains(output, "Context refreshed.") {
		t.Fatalf("expected refresh completion summary, got %q", output)
	}
	if got.refreshOutput {
		t.Fatal("expected refresh output flag to clear")
	}
}

func TestIdleOutputRendersWordmark(t *testing.T) {
	model := newTestTUIModel(t)
	model.setOutput()

	content := model.outputContent(90, 18)
	plain := stripANSI(content)
	if !strings.Contains(plain, "ggggg") {
		t.Fatalf("expected pretty idle wordmark, got %q", content)
	}
	if !strings.Contains(plain, "Select a pod") {
		t.Fatalf("expected idle hint, got %q", content)
	}
}

func TestStatePathUsesConfigDirOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GCPEASY_CONFIG_DIR", dir)

	path, err := tuiCachePath()
	if err != nil {
		t.Fatalf("expected state path: %v", err)
	}
	want := filepath.Join(dir, "tui-state.json")
	if path != want {
		t.Fatalf("expected path %q, got %q", want, path)
	}

	preferencesPath, err := tuiPreferencesPath()
	if err != nil {
		t.Fatalf("expected preferences path: %v", err)
	}
	preferencesWant := filepath.Join(dir, "tui-preferences.json")
	if preferencesPath != preferencesWant {
		t.Fatalf("expected preferences path %q, got %q", preferencesWant, preferencesPath)
	}
}

func TestStatePathUsesXDGConfigHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GCPEASY_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", dir)

	path, err := tuiCachePath()
	if err != nil {
		t.Fatalf("expected state path: %v", err)
	}
	want := filepath.Join(dir, "gcpeasy", "tui-state.json")
	if path != want {
		t.Fatalf("expected path %q, got %q", want, path)
	}
}

func TestDefaultStatePathUsesDotConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GCPEASY_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)

	path, err := tuiCachePath()
	if err != nil {
		t.Fatalf("expected state path: %v", err)
	}
	want := filepath.Join(home, ".config", "gcpeasy", "tui-state.json")
	if path != want {
		t.Fatalf("expected path %q, got %q", want, path)
	}
}

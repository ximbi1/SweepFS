package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"sweepfs/internal/config"
	"sweepfs/internal/domain"
	"sweepfs/internal/services"
	"sweepfs/internal/state"
)

type Model struct {
	state                *state.State
	scanner              services.Scanner
	actions              services.Actions
	progress             services.ProgressProvider
	snapshot             services.SnapshotProvider
	invalid              services.Invalidator
	previewer            services.ActionPreviewer
	actionProgress       services.ActionProgressProvider
	keys                 KeyMap
	showHelp             bool
	status               string
	scanning             bool
	request              string
	pending              string
	scanCtx              context.Context
	cancel               context.CancelFunc
	width                int
	height               int
	viewTop              int
	progressCount        int64
	confirming           bool
	confirmStep          int
	pendingAction        services.ActionType
	pendingPreview       services.ActionPreview
	pendingDestination   string
	pendingFocus         string
	awaitingDestination  bool
	capturingDestination bool
	destinationInput     string
	completionSuggestions []string
	backupBaseDestination string
	awaitingBackupName    bool
	backupNameInput       string
	awaitingCompression   bool
	filterInputMode       string
	filterInputValue      string
	actionRunning        bool
	actionProgressCount  int
}

type ConfigProvider interface {
	ConfigSnapshot() config.Config
}

func NewModel(appState *state.State, scanner services.Scanner, actions services.Actions) Model {
	ctx, cancel := context.WithCancel(context.Background())
	return Model{
		state:          appState,
		scanner:        scanner,
		actions:        actions,
		progress:       progressProvider(scanner),
		snapshot:       snapshotProvider(scanner),
		invalid:        invalidator(scanner),
		previewer:      actionPreviewer(actions),
		actionProgress: actionProgressProvider(actions),
		keys:           DefaultKeyMap(),
		status:         "Ready - press s to scan",
		scanning:       false,
		request:        appState.Path,
		scanCtx:        ctx,
		cancel:         cancel,
		width:          100,
		height:         30,
	}
}

func (model Model) WithStatus(message string) Model {
	if message != "" {
		model.status = message
	}
	return model
}

func (model Model) ConfigSnapshot() config.Config {
	return config.Config{
		Path:            model.state.Path,
		ShowHidden:      model.state.Prefs.ShowHidden,
		SafeMode:        model.state.Prefs.SafeMode,
		SortMode:        model.state.Prefs.SortMode,
		Theme:           model.state.Prefs.Theme,
		KeyBindings:     model.state.KeyBindings,
		LastDestination: model.state.LastDestination,
	}
}

func (model Model) Init() tea.Cmd {
	return nil
}

func (model Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		return model.handleKey(typed)
	case tea.WindowSizeMsg:
		model.width = typed.Width
		model.height = typed.Height
		model.ensureCursorVisible()
		return model, nil
	case scanResultMsg:
		model.scanning = false
		if model.cancel != nil {
			model.cancel = nil
		}
		if typed.err != nil {
			if errors.Is(typed.err, context.Canceled) {
				model.status = "Scan cancelled"
				return model, nil
			}
			model.status = fmt.Sprintf("Scan error: %v", typed.err)
			return model, nil
		}
		if model.snapshot != nil {
			model.state.SetTree(model.snapshot.Snapshot())
		}
		if model.pendingFocus != "" {
			model.state.SetCurrent(model.pendingFocus)
			model.pendingFocus = ""
		}
		if model.pending != "" {
			model.state.ToggleExpanded(model.pending)
			model.pending = ""
		}
		model.status = fmt.Sprintf("Scan complete (%s)", typed.result.Duration)
		model.ensureCursorVisible()
		model.ensureDetailCounts()
		return model, nil
	case scanProgressMsg:
		if typed.progress.ErrMessage != "" {
			model.status = fmt.Sprintf("Scan warning: %s", typed.progress.ErrMessage)
			return model, model.progressCmd()
		}
		if typed.progress.Completed {
			if model.scanning {
				return model, model.progressCmd()
			}
			return model, nil
		}
		model.progressCount = typed.progress.Scanned
		if typed.progress.Current != "" {
			model.status = fmt.Sprintf("Scanning... %d items (%s)", typed.progress.Scanned, typed.progress.Current)
		} else {
			model.status = fmt.Sprintf("Scanning... %d items", typed.progress.Scanned)
		}
		return model, model.progressCmd()
	case actionResultMsg:
		if typed.err != nil {
			model.status = fmt.Sprintf("Action error: %v", typed.err)
			return model, nil
		}
		model.actionRunning = false
		model.actionProgressCount = 0
		model.status = fmt.Sprintf("%s (%d ok, %d failed)", typed.result.Message, typed.result.SuccessCount, typed.result.FailureCount)
		return model, nil
	case actionPreviewMsg:
		if typed.err != nil {
			model.status = fmt.Sprintf("Preview error: %v", typed.err)
			model.confirming = false
			model.capturingDestination = false
			return model, nil
		}
		model.pendingPreview = typed.preview
		model.confirming = true
		model.confirmStep = 1
		model.status = previewPrompt(typed.preview, 1)
		return model, nil
	case actionProgressMsg:
		if typed.progress.ErrMessage != "" {
			model.status = fmt.Sprintf("Action warning: %s", typed.progress.ErrMessage)
			return model, model.actionProgressCmd()
		}
		if typed.progress.Completed {
			return model, nil
		}
		model.actionProgressCount = typed.progress.Processed
		if typed.progress.Current != "" {
			model.status = fmt.Sprintf("%s %d items", strings.ToUpper(string(typed.progress.Type)), typed.progress.Processed)
		}
		return model, model.actionProgressCmd()
	default:
		return model, nil
	}
}

func (model Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, model.keys.Quit):
		model = model.cancelScan("")
		return model, tea.Quit
	case key.Matches(msg, model.keys.Help):
		model.showHelp = !model.showHelp
		return model, nil
	case model.confirming && key.Matches(msg, model.keys.Confirm):
		return model.confirmAction()
	case model.confirming && key.Matches(msg, model.keys.Cancel):
		model.confirming = false
		model.confirmStep = 0
		model.status = "Action cancelled"
		return model, nil
	case model.awaitingCompression:
		return model.handleCompressionChoice(msg)
	case model.awaitingBackupName:
		return model.handleBackupNameInput(msg)
	case model.filterInputMode != "":
		return model.handleFilterInput(msg)
	case model.awaitingDestination && key.Matches(msg, model.keys.Paste):
		model.awaitingDestination = false
		model.pendingDestination = model.state.CurrentPath()
		return model.finalizeDestination(model.pendingDestination)
	case model.awaitingDestination && msg.Type == tea.KeyRunes:
		model.awaitingDestination = false
		model.capturingDestination = true
		model.destinationInput += string(msg.Runes)
		model.status = fmt.Sprintf("Destination: %s", model.destinationInput)
		model.updateCompletionSuggestions()
		return model, nil
	case model.awaitingDestination && (msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete):
		model.awaitingDestination = false
		model.capturingDestination = true
		model.destinationInput = ""
		model.status = "Destination: "
		model.completionSuggestions = nil
		return model, nil
	case model.awaitingDestination && key.Matches(msg, model.keys.Cancel):
		model.awaitingDestination = false
		model.status = "Destination entry cancelled"
		return model, nil
	case model.capturingDestination:
		return model.handleDestinationInput(msg)
	case key.Matches(msg, model.keys.Up):
		if model.state.Cursor > 0 {
			model.state.Cursor--
			model.ensureCursorVisible()
			model.ensureDetailCounts()
		}
		return model, nil
	case key.Matches(msg, model.keys.Down):
		visible := model.state.VisibleNodes()
		if model.state.Cursor < len(visible)-1 {
			model.state.Cursor++
			model.ensureCursorVisible()
			model.ensureDetailCounts()
		}
		return model, nil
	case key.Matches(msg, model.keys.Select):
		if node := model.state.CurrentNode(); node != nil {
			model.state.ToggleSelection(node.ID)
		}
		return model, nil
	case key.Matches(msg, model.keys.Delete):
		return model.beginAction(services.ActionDelete)
	case key.Matches(msg, model.keys.Move):
		return model.beginAction(services.ActionMove)
	case key.Matches(msg, model.keys.Copy):
		return model.beginAction(services.ActionCopy)
	case key.Matches(msg, model.keys.Backup):
		return model.beginAction(services.ActionBackup)
	case key.Matches(msg, model.keys.Enter):
		node := model.state.CurrentNode()
		if node == nil || node.Type != domain.NodeDir {
			return model, nil
		}
		if !node.Scanned {
			model.status = "Not scanned - press s to scan"
			return model, nil
		}
		model.state.ToggleExpanded(node.ID)
		model.ensureCursorVisible()
		model.ensureDetailCounts()
		return model, nil
	case key.Matches(msg, model.keys.Right):
		node := model.state.CurrentNode()
		if node == nil || node.Type != domain.NodeDir {
			return model, nil
		}
		if !node.Scanned {
			if err := model.state.LoadListing(node.Path); err != nil {
				model.status = fmt.Sprintf("List error: %v", err)
				return model, nil
			}
			model.status = "Ready - press s to scan"
			model.ensureCursorVisible()
			return model, nil
		}
		model.state.SetCurrent(node.ID)
		model.ensureCursorVisible()
		model.ensureDetailCounts()
		return model, nil
	case key.Matches(msg, model.keys.Back):
		model = model.cancelScan("Scan cancelled")
		if model.state.LeaveDir() {
			model.ensureCursorVisible()
			return model, nil
		}
		currentPath := model.state.CurrentPath()
		parentPath := parentDirPath(currentPath)
		if parentPath == "" {
			return model, nil
		}
		if err := model.state.LoadListing(parentPath); err != nil {
			model.status = fmt.Sprintf("List error: %v", err)
			return model, nil
		}
		model.status = "Ready - press s to scan"
		model.ensureCursorVisible()
		model.ensureDetailCounts()
		return model, nil
	case key.Matches(msg, model.keys.Left):
		model = model.cancelScan("Scan cancelled")
		if model.state.LeaveDir() {
			model.ensureCursorVisible()
			return model, nil
		}
		currentPath := model.state.CurrentPath()
		parentPath := parentDirPath(currentPath)
		if parentPath == "" {
			return model, nil
		}
		if err := model.state.LoadListing(parentPath); err != nil {
			model.status = fmt.Sprintf("List error: %v", err)
			return model, nil
		}
		model.status = "Ready - press s to scan"
		model.ensureCursorVisible()
		return model, nil
	case key.Matches(msg, model.keys.Refresh):
		if model.invalid != nil {
			if node := model.state.CurrentNode(); node != nil {
				model.invalid.Invalidate(node.Path)
				return model.beginScan(node.Path, node.ID, node.ID)
			}
		}
		return model, nil
	case key.Matches(msg, model.keys.Sort):
		model.state.ToggleSortMode()
		model.ensureCursorVisible()
		return model, nil
	case key.Matches(msg, model.keys.Scan):
		path := model.state.CurrentPath()
		model.scanning = true
		model.status = fmt.Sprintf("Scanning... %s", path)
		return model.beginScan(path, "", path)
	case key.Matches(msg, model.keys.Hidden):
		model.state.ToggleShowHidden()
		path := model.state.CurrentPath()
		if model.invalid != nil {
			model.invalid.Invalidate(path)
		}
		if err := model.state.LoadListing(path); err != nil {
			model.status = fmt.Sprintf("List error: %v", err)
			return model, nil
		}
		model.status = "Ready - press s to scan"
		model.ensureCursorVisible()
		model.ensureDetailCounts()
		return model, nil
	case key.Matches(msg, model.keys.Search):
		model.filterInputMode = "search"
		model.filterInputValue = model.state.SearchQuery
		model.status = fmt.Sprintf("Search: %s", model.filterInputValue)
		return model, nil
	case key.Matches(msg, model.keys.ExtFilter):
		model.filterInputMode = "ext"
		model.filterInputValue = model.state.FilterExt
		model.status = fmt.Sprintf("Extension: %s", model.filterInputValue)
		return model, nil
	case key.Matches(msg, model.keys.SizeFilter):
		model.filterInputMode = "size"
		model.filterInputValue = formatSizeLabel(model.state.MinSizeBytes)
		model.status = fmt.Sprintf("Min size: %s", model.filterInputValue)
		return model, nil
	case key.Matches(msg, model.keys.ClearFilter):
		model.state.ClearFilters()
		model.status = "Filters cleared"
		model.ensureCursorVisible()
		return model, nil
	default:
		return model, nil
	}
}

func (model Model) beginAction(actionType services.ActionType) (tea.Model, tea.Cmd) {
	if model.actionRunning {
		model.status = "Action already running"
		return model, nil
	}
	if actionType == services.ActionMove || actionType == services.ActionCopy || actionType == services.ActionBackup {
		model.awaitingDestination = true
		model.capturingDestination = false
		model.pendingAction = actionType
		model.destinationInput = model.state.LastDestination
		model.completionSuggestions = nil
		model.backupBaseDestination = ""
		model.awaitingBackupName = false
		model.backupNameInput = ""
		model.awaitingCompression = false
		if model.destinationInput == "" {
			model.status = "Navigate to destination and press p, or type a path"
		} else {
			model.capturingDestination = true
			model.status = fmt.Sprintf("Destination: %s", model.destinationInput)
			model.updateCompletionSuggestions()
		}
		return model, nil
	}
	return model.requestPreview(actionType, "")
}

func (model Model) requestPreview(actionType services.ActionType, destination string) (tea.Model, tea.Cmd) {
	if model.previewer == nil {
		model.status = "Preview unavailable"
		return model, nil
	}
	paths := model.state.SelectedPaths()
	request := services.ActionRequest{
		Type:        actionType,
		SourcePaths: paths,
		Destination: destination,
		SafeMode:    model.state.Prefs.SafeMode,
	}
	model.pendingAction = actionType
	model.pendingDestination = destination
	return model, func() tea.Msg {
		preview, err := model.previewer.Preview(context.Background(), request)
		return actionPreviewMsg{preview: preview, err: err}
	}
}

func (model Model) confirmAction() (tea.Model, tea.Cmd) {
	preview := model.pendingPreview
	confirmToken := "confirm"
	if preview.Type == services.ActionDelete && preview.TotalDirs > 0 {
		if model.confirmStep == 1 {
			model.confirmStep = 2
			model.status = previewPrompt(preview, 2)
			return model, nil
		}
		confirmToken = "confirm-recursive"
	}
	model.confirming = false
	model.confirmStep = 0
	model.actionRunning = true
	model.actionProgressCount = 0
	model.status = fmt.Sprintf("%s in progress", strings.ToUpper(string(preview.Type)))
	paths := model.state.SelectedPaths()
	request := services.ActionRequest{
		Type:         preview.Type,
		SourcePaths:  paths,
		Destination:  model.pendingDestination,
		SafeMode:     model.state.Prefs.SafeMode,
		ConfirmToken: confirmToken,
	}
	return model, tea.Batch(model.actionExecuteCmd(request), model.actionProgressCmd())
}

func (model Model) actionExecuteCmd(request services.ActionRequest) tea.Cmd {
	return func() tea.Msg {
		result, err := model.actions.Execute(context.Background(), request)
		return actionResultMsg{result: result, err: err}
	}
}

func (model Model) actionProgressCmd() tea.Cmd {
	if model.actionProgress == nil {
		return nil
	}
	return func() tea.Msg {
		for {
			channel := model.actionProgress.ActionProgress()
			if channel == nil {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			progress, ok := <-channel
			if !ok {
				return actionProgressMsg{progress: services.ActionProgress{Completed: true}}
			}
			return actionProgressMsg{progress: progress}
		}
	}
}

func (model Model) handleDestinationInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		model.capturingDestination = false
		model.status = "Destination entry cancelled"
		return model, nil
	case tea.KeyEnter:
		model.capturingDestination = false
		destination := strings.TrimSpace(model.destinationInput)
		model.state.LastDestination = destination
		model.awaitingDestination = false
		return model.finalizeDestination(destination)
	case tea.KeyTab:
		model.destinationInput, model.completionSuggestions = completePath(model.destinationInput)
		model.status = fmt.Sprintf("Destination: %s", model.destinationInput)
		return model, nil
	case tea.KeyBackspace, tea.KeyDelete:
		if len(model.destinationInput) > 0 {
			model.destinationInput = model.destinationInput[:len(model.destinationInput)-1]
		}
		model.updateCompletionSuggestions()
	default:
		if msg.Type == tea.KeyRunes {
			model.destinationInput += string(msg.Runes)
			model.updateCompletionSuggestions()
		}
	}
	model.status = fmt.Sprintf("Destination: %s", model.destinationInput)
	return model, nil
}

func (model Model) finalizeDestination(destination string) (tea.Model, tea.Cmd) {
	if model.pendingAction == services.ActionBackup {
		model.backupBaseDestination = destination
		model.awaitingBackupName = true
		model.backupNameInput = ""
		model.status = "Backup name:"
		return model, nil
	}
	return model.requestPreview(model.pendingAction, destination)
}

func (model Model) handleBackupNameInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		model.awaitingBackupName = false
		model.status = "Backup name cancelled"
		return model, nil
	case tea.KeyEnter:
		name := strings.TrimSpace(model.backupNameInput)
		if name == "" {
			model.status = "Backup name required"
			return model, nil
		}
		model.awaitingBackupName = false
		model.awaitingCompression = true
		model.status = "Compress backup? (y/n)"
		return model, nil
	case tea.KeyBackspace, tea.KeyDelete:
		if len(model.backupNameInput) > 0 {
			model.backupNameInput = model.backupNameInput[:len(model.backupNameInput)-1]
		}
	default:
		if msg.Type == tea.KeyRunes {
			model.backupNameInput += string(msg.Runes)
		}
	}
	model.status = fmt.Sprintf("Backup name: %s", model.backupNameInput)
	return model, nil
}

func (model Model) handleCompressionChoice(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		model.awaitingCompression = false
		model.status = "Compression choice cancelled"
		return model, nil
	}
	if msg.Type != tea.KeyRunes {
		return model, nil
	}
	choice := strings.ToLower(string(msg.Runes))
	if choice != "y" && choice != "n" {
		return model, nil
	}
	model.awaitingCompression = false
	name := strings.TrimSpace(model.backupNameInput)
	if name == "" {
		model.status = "Backup name required"
		return model, nil
	}
	destination := filepath.Join(model.backupBaseDestination, name)
	if choice == "y" {
		destination += ".tar.gz"
	}
	return model.requestPreview(model.pendingAction, destination)
}

func (model Model) handleFilterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		model.filterInputMode = ""
		model.filterInputValue = ""
		model.status = "Filter cancelled"
		return model, nil
	case tea.KeyEnter:
		mode := model.filterInputMode
		value := strings.TrimSpace(model.filterInputValue)
		model.filterInputMode = ""
		switch mode {
		case "search":
			model.state.SearchQuery = value
		case "ext":
			model.state.FilterExt = value
		case "size":
			model.state.MinSizeBytes = parseSizeInput(value)
		}
		model.ensureCursorVisible()
		model.status = "Filter applied"
		return model, nil
	case tea.KeyBackspace, tea.KeyDelete:
		if len(model.filterInputValue) > 0 {
			model.filterInputValue = model.filterInputValue[:len(model.filterInputValue)-1]
		}
	default:
		if msg.Type == tea.KeyRunes {
			model.filterInputValue += string(msg.Runes)
		}
	}
	model.status = fmt.Sprintf("%s: %s", filterLabel(model.filterInputMode), model.filterInputValue)
	return model, nil
}

func parseSizeInput(input string) int64 {
	trimmed := strings.TrimSpace(strings.ToLower(input))
	if trimmed == "" {
		return 0
	}
	value := trimmed
	multiplier := int64(1)
	if strings.HasSuffix(trimmed, "kb") {
		value = strings.TrimSuffix(trimmed, "kb")
		multiplier = 1000
	} else if strings.HasSuffix(trimmed, "k") {
		value = strings.TrimSuffix(trimmed, "k")
		multiplier = 1000
	} else if strings.HasSuffix(trimmed, "mb") {
		value = strings.TrimSuffix(trimmed, "mb")
		multiplier = 1000 * 1000
	} else if strings.HasSuffix(trimmed, "m") {
		value = strings.TrimSuffix(trimmed, "m")
		multiplier = 1000 * 1000
	} else if strings.HasSuffix(trimmed, "gb") {
		value = strings.TrimSuffix(trimmed, "gb")
		multiplier = 1000 * 1000 * 1000
	} else if strings.HasSuffix(trimmed, "g") {
		value = strings.TrimSuffix(trimmed, "g")
		multiplier = 1000 * 1000 * 1000
	} else if strings.HasSuffix(trimmed, "tb") {
		value = strings.TrimSuffix(trimmed, "tb")
		multiplier = 1000 * 1000 * 1000 * 1000
	} else if strings.HasSuffix(trimmed, "t") {
		value = strings.TrimSuffix(trimmed, "t")
		multiplier = 1000 * 1000 * 1000 * 1000
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0
	}
	return int64(parsed * float64(multiplier))
}

func filterLabel(mode string) string {
	switch mode {
	case "search":
		return "Search"
	case "ext":
		return "Extension"
	case "size":
		return "Min size"
	default:
		return "Filter"
	}
}

func formatSizeLabel(size int64) string {
	if size <= 0 {
		return ""
	}
	const unit = 1000
	if size < unit {
		return fmt.Sprintf("%dB", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	value := float64(size) / float64(div)
	units := []string{"KB", "MB", "GB", "TB", "PB", "EB"}
	return fmt.Sprintf("%.1f%s", value, units[exp])
}

func (model Model) beginScan(path string, pendingID string, focusID string) (Model, tea.Cmd) {
	model = model.cancelScan("Scan cancelled")
	model.state.Path = path
	if model.state.Tree.RootID == "" {
		if err := model.state.LoadListing(path); err != nil {
			model.status = fmt.Sprintf("List error: %v", err)
			return model, nil
		}
	}
	if focusID != "" {
		if _, ok := model.state.Tree.Nodes[focusID]; ok {
			model.state.Current = focusID
			model.state.Cursor = 0
			model.state.Expanded[focusID] = true
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	model.scanCtx = ctx
	model.cancel = cancel
	model.scanning = true
	model.request = path
	model.pending = pendingID
	model.pendingFocus = focusID
	model.progressCount = 0
	model.status = fmt.Sprintf("Scanning... %s", path)
	return model, tea.Batch(model.scanCmd(ctx, path), model.progressCmd())
}

func (model Model) scanCmd(ctx context.Context, path string) tea.Cmd {
	request := services.ScanRequest{
		RootPath:   path,
		ShowHidden: model.state.Prefs.ShowHidden,
	}

	return func() tea.Msg {
		result, err := model.scanner.Scan(ctx, request)
		return scanResultMsg{result: result, err: err}
	}
}

func (model Model) progressCmd() tea.Cmd {
	if model.progress == nil {
		return nil
	}
	return func() tea.Msg {
		for {
			channel := model.progress.Progress()
			if channel == nil {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			progress, ok := <-channel
			if !ok {
				return scanProgressMsg{progress: services.ScanProgress{Completed: true}}
			}
			return scanProgressMsg{progress: progress}
		}
	}
}

func (model Model) cancelScan(message string) Model {
	if model.cancel != nil {
		model.cancel()
		model.cancel = nil
	}
	if message != "" {
		model.status = message
	}
	model.scanning = false
	model.progressCount = 0
	return model
}

func progressProvider(scanner services.Scanner) services.ProgressProvider {
	provider, _ := scanner.(services.ProgressProvider)
	return provider
}

func snapshotProvider(scanner services.Scanner) services.SnapshotProvider {
	provider, _ := scanner.(services.SnapshotProvider)
	return provider
}

func invalidator(scanner services.Scanner) services.Invalidator {
	provider, _ := scanner.(services.Invalidator)
	return provider
}

func actionPreviewer(actions services.Actions) services.ActionPreviewer {
	previewer, _ := actions.(services.ActionPreviewer)
	return previewer
}

func actionProgressProvider(actions services.Actions) services.ActionProgressProvider {
	provider, _ := actions.(services.ActionProgressProvider)
	return provider
}

func previewPrompt(preview services.ActionPreview, step int) string {
	summary := fmt.Sprintf("%s on %d files, %d dirs, %s", strings.ToUpper(string(preview.Type)), preview.TotalFiles, preview.TotalDirs, formatSize(preview.TotalBytes))
	if step == 2 {
		return summary + " - confirm recursive delete (y/n)"
	}
	return summary + " - confirm (y/n)"
}

func (model *Model) ensureCursorVisible() {
	visible := model.state.VisibleNodes()
	if len(visible) == 0 {
		model.state.Cursor = 0
		model.viewTop = 0
		return
	}
	if model.state.Cursor >= len(visible) {
		model.state.Cursor = len(visible) - 1
	}
	if model.state.Cursor < 0 {
		model.state.Cursor = 0
	}
	listHeight := model.listHeight()
	if listHeight <= 0 {
		return
	}
	if model.state.Cursor < model.viewTop {
		model.viewTop = model.state.Cursor
	}
	if model.state.Cursor >= model.viewTop+listHeight {
		model.viewTop = model.state.Cursor - listHeight + 1
	}
	maxTop := len(visible) - listHeight
	if maxTop < 0 {
		maxTop = 0
	}
	if model.viewTop > maxTop {
		model.viewTop = maxTop
	}
}

func (model *Model) listHeight() int {
	height := model.height - 6
	if height < 5 {
		return height
	}
	return height
}

func parentDirPath(path string) string {
	if path == "" {
		return ""
	}
	parent := filepath.Dir(path)
	if parent == path {
		return ""
	}
	return parent
}

func (model *Model) ensureDetailCounts() {
	node := model.state.CurrentNode()
	model.state.EnsureShallowCounts(node)
}

func (model *Model) updateCompletionSuggestions() {
	_, suggestions := completePath(model.destinationInput)
	model.completionSuggestions = suggestions
}

func completePath(input string) (string, []string) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return trimmed, nil
	}
	dir := filepath.Dir(trimmed)
	base := filepath.Base(trimmed)
	if strings.HasSuffix(trimmed, string(filepath.Separator)) {
		dir = trimmed
		base = ""
	}
	if dir == "." {
		dir = ""
	}
	readDir := dir
	if readDir == "" {
		readDir = "."
	}
	entries, err := os.ReadDir(readDir)
	if err != nil {
		return input, nil
	}
	matches := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, base) {
			matches = append(matches, name)
		}
	}
	if len(matches) == 0 {
		return input, nil
	}
	completed := commonPrefix(matches)
	if dir != "" {
		completed = filepath.Join(dir, completed)
	}
	if len(matches) == 1 && entriesHasDir(entries, matches[0]) {
		completed += string(filepath.Separator)
	}
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		if dir != "" {
			paths = append(paths, filepath.Join(dir, match))
		} else {
			paths = append(paths, match)
		}
	}
	return completed, paths
}

func commonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	prefix := values[0]
	for _, value := range values[1:] {
		for !strings.HasPrefix(value, prefix) && prefix != "" {
			prefix = prefix[:len(prefix)-1]
		}
		if prefix == "" {
			return ""
		}
	}
	return prefix
}

func entriesHasDir(entries []os.DirEntry, name string) bool {
	for _, entry := range entries {
		if entry.Name() == name {
			return entry.IsDir()
		}
	}
	return false
}

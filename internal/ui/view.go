package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"

	"sweepfs/internal/domain"
	"sweepfs/internal/state"
)

type uiStyles struct {
	headerStyle   lipgloss.Style
	mutedStyle    lipgloss.Style
	statusStyle   lipgloss.Style
	warnStyle     lipgloss.Style
	cursorStyle   lipgloss.Style
	selectedStyle lipgloss.Style
	panelBorder   lipgloss.Style
}

func stylesFor(model Model) uiStyles {
	if strings.ToLower(model.state.Prefs.Theme) == "light" {
		return uiStyles{
			headerStyle:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("235")),
			mutedStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("242")),
			statusStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("25")).Bold(true),
			warnStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("124")).Bold(true),
			cursorStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("90")).Bold(true),
			selectedStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("28")).Bold(true),
			panelBorder:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1),
		}
	}
	return uiStyles{
		headerStyle:   lipgloss.NewStyle().Bold(true),
		mutedStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		statusStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("69")).Bold(true),
		warnStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("204")).Bold(true),
		cursorStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true),
		selectedStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true),
		panelBorder:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1),
	}
}

func (model Model) View() string {
	styles := stylesFor(model)
	if model.showHelp {
		return renderHelpView(model, styles)
	}

	body := renderBody(model, styles)
	footer := renderFooter(model, styles)
	return strings.Join([]string{body, footer}, "\n")
}

func renderBody(model Model, styles uiStyles) string {
	visible := model.state.VisibleNodes()
	bodyHeight := model.listHeight()
	if bodyHeight < 3 {
		bodyHeight = 3
	}

	leftWidth, rightWidth, showRight := splitPanels(model.width)
	left := renderTreePanel(model, styles, visible, bodyHeight, leftWidth)
	if !showRight {
		return left
	}
	sep := lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render("│")
	right := renderDetailPanel(model, styles, rightWidth, bodyHeight)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right)
}

func renderFooter(model Model, styles uiStyles) string {
	statusLine := trimStatus(model.status, model.width)
	if model.scanning {
		statusLine = fmt.Sprintf("%s  %s", statusLine, progressBar(model.progressCount, 18))
	}
	if model.actionRunning {
		statusLine = fmt.Sprintf("%s  %s", statusLine, progressBar(int64(model.actionProgressCount), 18))
	}
	statusStyle := styles.mutedStyle
	if strings.Contains(strings.ToLower(model.status), "error") || strings.Contains(strings.ToLower(model.status), "warning") {
		statusStyle = styles.warnStyle
	}
	statusLine = statusStyle.Render(statusLine)

	selectedCount, selectedSize := model.state.SelectionSummary()
	selectionInfo := fmt.Sprintf("Selected: %d (%s)", selectedCount, formatSize(selectedSize))
	sortInfo := fmt.Sprintf("Sort: %s", strings.ToUpper(string(model.state.Prefs.SortMode)))
	hiddenInfo := "Hidden: off"
	if model.state.Prefs.ShowHidden {
		hiddenInfo = "Hidden: on"
	}
	filterInfo := filterSummary(model)
	left := fmt.Sprintf("%s  %s  %s%s", selectionInfo, sortInfo, hiddenInfo, filterInfo)
	keys := "↑/↓ move  → enter  ← up  enter expand  s scan  / search  e ext  z min  x clear  o sort  h hidden  p paste  r refresh  ? help  q quit"
	if model.confirming {
		keys = "y confirm  n cancel"
	}
	if model.awaitingDestination {
		keys = "navigate + p paste  or type path  tab complete"
	}
	if model.capturingDestination {
		keys = "type destination  tab complete  enter confirm  esc cancel"
	}
	if model.awaitingBackupName {
		keys = "type backup name  enter confirm  esc cancel"
	}
	if model.awaitingCompression {
		keys = "compress? y/n"
	}
	footerLine := padLine(left, keys, model.width)
	return strings.Join([]string{statusLine, styles.mutedStyle.Render(footerLine)}, "\n")
}

func renderTreePanel(model Model, styles uiStyles, visible []state.VisibleNode, height, width int) string {
	if width < 20 {
		width = 20
	}
	contentWidth := maxInt(width-2, 10)
	path := currentPath(model)
	crumbs := breadcrumbs(path)
	status := "IDLE"
	if model.scanning {
		status = "SCANNING"
	}
	headerLine := padLine(styles.headerStyle.Render("SweepFS")+"  "+crumbs, styles.statusStyle.Render(status), contentWidth)
	listHeight := height - 1
	if listHeight < 1 {
		listHeight = 1
	}
	if len(visible) == 0 {
		message := "Not scanned - press s"
		if model.scanning {
			message = "Scanning..."
		}
		lines := []string{headerLine, message}
		for i := 0; i < maxInt(listHeight-1, 0); i++ {
			lines = append(lines, "")
		}
		return styles.panelBorder.Width(contentWidth).Render(strings.Join(lines, "\n"))
	}
	start := clamp(model.viewTop, 0, maxInt(len(visible)-1, 0))
	end := start + listHeight
	if end > len(visible) {
		end = len(visible)
	}

	lines := make([]string, 0, height)
	lines = append(lines, headerLine)
	sizeWidth := 9
	for index := start; index < end; index++ {
		item := visible[index]
		node := item.Node
		indent := strings.Repeat("  ", item.Depth)
		icon := fileIcon(model, node)
		marker := "[ ]"
		if model.state.Selected[node.ID] {
			marker = styles.selectedStyle.Render("[x]")
		}
		name := node.Name
		if node.Type == domain.NodeDir {
			name += "/"
		}
		lineSize := fmt.Sprintf("%*s", sizeWidth, sizeLabel(node))
		line := fmt.Sprintf("%s %s %s%s %s", lineSize, marker, indent, icon, name)
		if index == model.state.Cursor {
			line = styles.cursorStyle.Render(line)
		}
		lines = append(lines, line)
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	content := strings.Join(lines, "\n")
	return styles.panelBorder.Width(contentWidth).Render(content)
}

func renderDetailPanel(model Model, styles uiStyles, width, height int) string {
	if model.confirming {
		return renderPreviewPanel(model, styles, width, height)
	}
	if model.awaitingDestination || model.capturingDestination {
		return renderDestinationPanel(model, styles, width, height)
	}
	if model.awaitingBackupName || model.awaitingCompression {
		return renderBackupPanel(model, styles, width, height)
	}
	node := model.state.CurrentNode()
	if node == nil {
		return styles.panelBorder.Width(maxInt(width-2, 10)).Render("No selection")
	}
	contentWidth := maxInt(width-2, 10)
	mod := "-"
	if !node.ModTime.IsZero() {
		mod = node.ModTime.Format(time.RFC822)
	}
	lines := []string{
		styles.headerStyle.Render("Path"),
		node.Path,
		"",
		styles.headerStyle.Render("Size"),
		fmt.Sprintf("Direct: %s", formatSize(node.SizeBytes)),
		fmt.Sprintf("Total : %s", formatSize(sizeFor(node))),
	}
	if node.Type == domain.NodeDir {
		folders := node.ChildCount
		files := node.FileCount
		if node.Scanned {
			if node.DirCount > 0 {
				folders = node.DirCount
			}
			files = node.FileCount
		}
		lines = append(lines, fmt.Sprintf("Folders: %d", folders))
		lines = append(lines, fmt.Sprintf("Files : %d", files))
	}
	lines = append(lines, "", styles.headerStyle.Render("Modified"), mod)

	content := strings.Join(lines, "\n")
	content = lipgloss.NewStyle().Width(contentWidth).Height(height).Render(content)
	return styles.panelBorder.Width(contentWidth).Render(content)
}

func renderDestinationPanel(model Model, styles uiStyles, width, height int) string {
	contentWidth := maxInt(width-2, 10)
	lines := []string{
		styles.headerStyle.Render("Destination"),
		model.destinationInput,
	}
	if len(model.completionSuggestions) > 0 {
		lines = append(lines, "", styles.headerStyle.Render("Suggestions"))
		max := 8
		if len(model.completionSuggestions) < max {
			max = len(model.completionSuggestions)
		}
		lines = append(lines, model.completionSuggestions[:max]...)
		if len(model.completionSuggestions) > max {
			lines = append(lines, "...")
		}
	}
	content := strings.Join(lines, "\n")
	content = lipgloss.NewStyle().Width(contentWidth).Height(height).Render(content)
	return styles.panelBorder.Width(contentWidth).Render(content)
}

func renderBackupPanel(model Model, styles uiStyles, width, height int) string {
	contentWidth := maxInt(width-2, 10)
	lines := []string{
		styles.headerStyle.Render("Backup"),
		fmt.Sprintf("Base: %s", model.backupBaseDestination),
		fmt.Sprintf("Name: %s", model.backupNameInput),
	}
	if model.awaitingCompression {
		lines = append(lines, "", "Compress backup? (y/n)")
	}
	content := strings.Join(lines, "\n")
	content = lipgloss.NewStyle().Width(contentWidth).Height(height).Render(content)
	return styles.panelBorder.Width(contentWidth).Render(content)
}

func renderPreviewPanel(model Model, styles uiStyles, width, height int) string {
	preview := model.pendingPreview
	lines := []string{
		styles.headerStyle.Render("Action Preview"),
		fmt.Sprintf("Type: %s", strings.ToUpper(string(preview.Type))),
		fmt.Sprintf("Files: %d", preview.TotalFiles),
		fmt.Sprintf("Dirs : %d", preview.TotalDirs),
		fmt.Sprintf("Size : %s", formatSize(preview.TotalBytes)),
	}
	if preview.Destination != "" {
		lines = append(lines, fmt.Sprintf("Dest : %s", preview.Destination))
	}
	if len(preview.Samples) > 0 {
		lines = append(lines, "", styles.headerStyle.Render("Samples"))
		for _, item := range preview.Samples {
			lines = append(lines, item)
		}
	}
	if len(preview.Warnings) > 0 {
		lines = append(lines, "", styles.headerStyle.Render("Warnings"))
		for _, warn := range preview.Warnings {
			lines = append(lines, warn)
		}
	}
	contentWidth := maxInt(width-2, 10)
	content := strings.Join(lines, "\n")
	content = lipgloss.NewStyle().Width(contentWidth).Height(height).Render(content)
	return styles.panelBorder.Width(contentWidth).Render(content)
}

func renderHelpView(model Model, styles uiStyles) string {
	bindings := []key.Binding{
		model.keys.Up,
		model.keys.Down,
		model.keys.Enter,
		model.keys.Right,
		model.keys.Back,
		model.keys.Left,
		model.keys.Select,
		model.keys.Delete,
		model.keys.Move,
		model.keys.Copy,
		model.keys.Backup,
		model.keys.Refresh,
		model.keys.Scan,
		model.keys.Sort,
		model.keys.Hidden,
		model.keys.Paste,
		model.keys.Search,
		model.keys.ExtFilter,
		model.keys.SizeFilter,
		model.keys.ClearFilter,
		model.keys.Confirm,
		model.keys.Cancel,
		model.keys.Help,
		model.keys.Quit,
	}

	lines := []string{styles.headerStyle.Render("SweepFS Help"), ""}
	lines = append(lines, styles.headerStyle.Render("Navigation"))
	lines = append(lines, "↑/↓ move cursor", "→ enter folder", "← go to parent", "enter expand/collapse")
	lines = append(lines, "", styles.headerStyle.Render("Selection"))
	lines = append(lines, "space toggle select", "selection counted in footer")
	lines = append(lines, "", styles.headerStyle.Render("Actions"))
	lines = append(lines, "s scan", "r refresh", "o sort", "h hidden", "/ search", "e ext filter", "z size filter", "x clear")
	lines = append(lines, "", styles.headerStyle.Render("Operations"))
	lines = append(lines, "d delete", "m move", "c copy", "b backup (name + compress)", "p paste dest")
	lines = append(lines, "", styles.headerStyle.Render("Safety"))
	lines = append(lines, "confirm with y", "cancel with n or esc", "blocked: /, $HOME, /etc, /usr, /var")
	lines = append(lines, "", styles.headerStyle.Render("Keys"))
	for _, binding := range bindings {
		keysLabel := strings.Join(binding.Keys(), ", ")
		lines = append(lines, fmt.Sprintf("%-18s %s", keysLabel, binding.Help().Desc))
	}
	lines = append(lines, "", "Press ? to close help")
	content := strings.Join(lines, "\n")
	width := model.width
	if width <= 0 {
		width = 80
	}
	return styles.panelBorder.Width(maxInt(width-2, 10)).Render(content)
}

func currentPath(model Model) string {
	return model.state.CurrentPath()
}

func breadcrumbs(path string) string {
	path = filepath.Clean(path)
	if path == "." {
		return "."
	}
	parts := strings.Split(path, string(filepath.Separator))
	if len(parts) == 0 {
		return path
	}
	if parts[0] == "" {
		parts[0] = string(filepath.Separator)
	}
	return strings.Join(parts, " › ")
}

func padLine(left, right string, width int) string {
	if width <= 0 {
		return left
	}
	space := width - lipgloss.Width(left) - lipgloss.Width(right)
	if space < 1 {
		return left + " " + right
	}
	return left + strings.Repeat(" ", space) + right
}

func splitPanels(width int) (int, int, bool) {
	if width < 80 {
		return width, 0, false
	}
	left := int(float64(width) * 0.6)
	if left < 40 {
		left = 40
	}
	right := width - left - 1
	if right < 30 {
		return width, 0, false
	}
	return left, right, true
}

func fileIcon(model Model, node *domain.Node) string {
	if node.Type == domain.NodeDir {
		if !node.Scanned {
			return "📁"
		}
		if model.state.IsExpanded(node.ID) {
			return "📂"
		}
		return "📁"
	}
	return "📄"
}

func formatSize(size int64) string {
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

func sizeLabel(node *domain.Node) string {
	if node.Type == domain.NodeDir && !node.Scanned {
		return "--"
	}
	return formatSize(sizeFor(node))
}

func sizeFor(node *domain.Node) int64 {
	if node.Type == domain.NodeDir {
		return node.AccumBytes
	}
	return node.SizeBytes
}

func progressBar(count int64, width int) string {
	if width <= 0 {
		return ""
	}
	pos := int(count % int64(width))
	filled := strings.Repeat("█", pos)
	gap := strings.Repeat("░", width-pos)
	return fmt.Sprintf("[%s%s]", filled, gap)
}

func trimStatus(message string, width int) string {
	if width <= 0 {
		return message
	}
	max := width - 4
	if max <= 0 || len(message) <= max {
		return message
	}
	return message[:max] + "..."
}

func filterSummary(model Model) string {
	parts := []string{}
	if model.state.SearchQuery != "" {
		parts = append(parts, fmt.Sprintf("Search:%s", model.state.SearchQuery))
	}
	if model.state.FilterExt != "" {
		parts = append(parts, fmt.Sprintf("Ext:%s", model.state.FilterExt))
	}
	if model.state.MinSizeBytes > 0 {
		parts = append(parts, fmt.Sprintf("Min:%s", formatSize(model.state.MinSizeBytes)))
	}
	if len(parts) == 0 {
		return ""
	}
	return "  Filters[" + strings.Join(parts, ", ") + "]"
}

func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

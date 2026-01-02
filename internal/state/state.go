package state

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sweepfs/internal/config"
	"sweepfs/internal/domain"
)

type Preferences struct {
	ShowHidden bool
	SafeMode   bool
	SortMode   domain.SortMode
	Theme      string
}

type State struct {
	Path            string
	Current         string
	Cursor          int
	Selected        map[string]bool
	Expanded        map[string]bool
	Prefs           Preferences
	Tree            domain.TreeIndex
	LastDestination string
	KeyBindings     map[string]string
	SearchQuery     string
	FilterExt       string
	MinSizeBytes    int64
}

func NewState(cfg config.Config) *State {
	return &State{
		Path:     cfg.Path,
		Current:  "",
		Cursor:   0,
		Selected: make(map[string]bool),
		Expanded: make(map[string]bool),
		Prefs: Preferences{
			ShowHidden: cfg.ShowHidden,
			SafeMode:   cfg.SafeMode,
			SortMode:   cfg.SortMode,
			Theme:      cfg.Theme,
		},
		Tree: domain.TreeIndex{
			Nodes: make(map[string]*domain.Node),
		},
		LastDestination: cfg.LastDestination,
		KeyBindings:     ensureBindings(cfg.KeyBindings),
		SearchQuery:     "",
		FilterExt:       "",
		MinSizeBytes:    0,
	}
}

func ensureBindings(bindings map[string]string) map[string]string {
	if bindings == nil {
		return map[string]string{}
	}
	return bindings
}

func (appState *State) SetTree(tree domain.TreeIndex) {
	appState.Tree = tree
	if appState.Current == "" {
		appState.Current = tree.RootID
	}
	if _, ok := appState.Tree.Nodes[appState.Current]; !ok {
		appState.Current = tree.RootID
	}

	filteredSelected := make(map[string]bool, len(appState.Selected))
	for id := range appState.Selected {
		if _, ok := appState.Tree.Nodes[id]; ok {
			filteredSelected[id] = true
		}
	}
	appState.Selected = filteredSelected

	filteredExpanded := make(map[string]bool, len(appState.Expanded))
	for id := range appState.Expanded {
		if _, ok := appState.Tree.Nodes[id]; ok {
			filteredExpanded[id] = true
		}
	}
	appState.Expanded = filteredExpanded
	if appState.Current != "" {
		appState.Expanded[appState.Current] = true
	}
}

func (appState *State) SetCurrent(id string) bool {
	if id == "" {
		return false
	}
	if _, ok := appState.Tree.Nodes[id]; !ok {
		return false
	}
	appState.Current = id
	appState.Cursor = 0
	appState.Expanded[id] = true
	return true
}

func (appState *State) LoadListing(path string) error {
	appState.Path = path
	appState.Current = ""
	appState.Cursor = 0
	appState.Selected = make(map[string]bool)
	appState.Expanded = make(map[string]bool)
	appState.Tree = domain.TreeIndex{Nodes: make(map[string]*domain.Node)}

	if path == "" {
		return nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}

	root := &domain.Node{
		ID:      path,
		Name:    filepath.Base(path),
		Path:    path,
		Type:    domain.NodeDir,
		Scanned: true,
	}
	if root.Name == "." || root.Name == string(filepath.Separator) {
		root.Name = path
	}
	appState.Tree.RootID = root.ID
	appState.Tree.Nodes[root.ID] = root
	appState.Current = root.ID
	appState.Expanded[root.ID] = true

	for _, entry := range entries {
		name := entry.Name()
		if !appState.Prefs.ShowHidden && strings.HasPrefix(name, ".") {
			continue
		}
		info, infoErr := entry.Info()
		child := &domain.Node{
			ID:       filepath.Join(path, name),
			Name:     name,
			Path:     filepath.Join(path, name),
			ParentID: root.ID,
			ModTime:  time.Time{},
		}
		if infoErr != nil {
			child.Type = domain.NodeFile
			child.SizeBytes = 0
			child.AccumBytes = 0
			child.FileCount = 1
		} else if info.IsDir() {
			child.Type = domain.NodeDir
			child.Scanned = false
			child.ModTime = info.ModTime()
			child.FileCount = 0
			child.DirCount = 0
		} else {
			child.Type = domain.NodeFile
			child.SizeBytes = info.Size()
			child.AccumBytes = info.Size()
			child.ModTime = info.ModTime()
			child.FileCount = 1
		}
		root.ChildrenIDs = append(root.ChildrenIDs, child.ID)
		if child.Type == domain.NodeDir {
			root.ChildCount++
			root.DirCount++
		} else {
			root.FileCount++
		}
		appState.Tree.Nodes[child.ID] = child
	}

	return nil
}

type VisibleNode struct {
	Node  *domain.Node
	Depth int
}

func (appState *State) VisibleNodes() []VisibleNode {
	rootID := appState.Current
	if rootID == "" {
		rootID = appState.Tree.RootID
	}
	root, ok := appState.Tree.Nodes[rootID]
	if !ok {
		return nil
	}
	visible := make([]VisibleNode, 0, len(appState.Tree.Nodes))
	appState.appendNode(&visible, root, 0)
	return visible
}

func (appState *State) CurrentNode() *domain.Node {
	visible := appState.VisibleNodes()
	if len(visible) == 0 || appState.Cursor < 0 || appState.Cursor >= len(visible) {
		return nil
	}
	return visible[appState.Cursor].Node
}

func (appState *State) CurrentPath() string {
	if node, ok := appState.Tree.Nodes[appState.Current]; ok {
		return node.Path
	}
	return appState.Path
}

func (appState *State) EnterDir(id string) bool {
	node, ok := appState.Tree.Nodes[id]
	if !ok || node.Type != domain.NodeDir || !node.Scanned {
		return false
	}
	appState.Current = id
	appState.Cursor = 0
	return true
}

func (appState *State) LeaveDir() bool {
	node, ok := appState.Tree.Nodes[appState.Current]
	if !ok || node.ParentID == "" {
		return false
	}
	appState.Current = node.ParentID
	appState.Cursor = 0
	return true
}

func (appState *State) ToggleExpanded(id string) bool {
	if id == "" {
		return false
	}
	appState.Expanded[id] = !appState.Expanded[id]
	return appState.Expanded[id]
}

func (appState *State) IsExpanded(id string) bool {
	return appState.Expanded[id]
}

func (appState *State) SelectionSummary() (int, int64) {
	var total int64
	count := len(appState.Selected)
	for id := range appState.Selected {
		if node, ok := appState.Tree.Nodes[id]; ok {
			if node.Type == domain.NodeDir {
				total += node.AccumBytes
			} else {
				total += node.SizeBytes
			}
		}
	}
	return count, total
}

func (appState *State) ToggleSortMode() domain.SortMode {
	switch appState.Prefs.SortMode {
	case domain.SortBySize:
		appState.Prefs.SortMode = domain.SortByName
	case domain.SortByName:
		appState.Prefs.SortMode = domain.SortByMod
	default:
		appState.Prefs.SortMode = domain.SortBySize
	}
	return appState.Prefs.SortMode
}

func (appState *State) ToggleShowHidden() bool {
	appState.Prefs.ShowHidden = !appState.Prefs.ShowHidden
	return appState.Prefs.ShowHidden
}

func (appState *State) EnsureShallowCounts(node *domain.Node) {
	if node == nil || node.Type != domain.NodeDir || node.Scanned {
		return
	}
	if node.ChildCount > 0 || node.FileCount > 0 {
		return
	}
	entries, err := os.ReadDir(node.Path)
	if err != nil {
		return
	}
	var dirs, files int
	for _, entry := range entries {
		name := entry.Name()
		if !appState.Prefs.ShowHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if entry.IsDir() {
			dirs++
		} else {
			files++
		}
	}
	node.ChildCount = dirs
	node.DirCount = dirs
	node.FileCount = files
}

func (appState *State) appendNode(visible *[]VisibleNode, node *domain.Node, depth int) {
	if node == nil {
		return
	}
	if !appState.Prefs.ShowHidden && isHiddenName(node.Name) && node.ID != appState.Tree.RootID {
		return
	}
	filtering := appState.SearchQuery != "" || appState.FilterExt != "" || appState.MinSizeBytes > 0
	if !filtering {
		*visible = append(*visible, VisibleNode{Node: node, Depth: depth})
		if node.Type != domain.NodeDir || !appState.IsExpanded(node.ID) {
			return
		}
		children := appState.sortedChildren(node)
		for _, child := range children {
			appState.appendNode(visible, child, depth+1)
		}
		return
	}
	if node.Type != domain.NodeDir {
		if appState.nodeMatches(node) {
			*visible = append(*visible, VisibleNode{Node: node, Depth: depth})
		}
		return
	}
	children := appState.sortedChildren(node)
	filteredChildren := make([]*domain.Node, 0, len(children))
	for _, child := range children {
		if appState.nodeMatches(child) {
			filteredChildren = append(filteredChildren, child)
			continue
		}
		if child.Type == domain.NodeDir && appState.dirHasMatch(child) {
			filteredChildren = append(filteredChildren, child)
		}
	}
	if node.ID == appState.Tree.RootID || appState.nodeMatches(node) || len(filteredChildren) > 0 {
		*visible = append(*visible, VisibleNode{Node: node, Depth: depth})
		if !appState.IsExpanded(node.ID) {
			return
		}
		for _, child := range filteredChildren {
			appState.appendNode(visible, child, depth+1)
		}
	}
}

func (appState *State) sortedChildren(node *domain.Node) []*domain.Node {
	children := make([]*domain.Node, 0, len(node.ChildrenIDs))
	for _, id := range node.ChildrenIDs {
		if child, ok := appState.Tree.Nodes[id]; ok {
			children = append(children, child)
		}
	}
	if len(children) < 2 {
		return children
	}
	less := func(i, j int) bool {
		if children[i].Type != children[j].Type {
			return children[i].Type == domain.NodeDir
		}
		switch appState.Prefs.SortMode {
		case domain.SortByName:
			return children[i].Name < children[j].Name
		case domain.SortByMod:
			return children[i].ModTime.After(children[j].ModTime)
		default:
			return sizeFor(children[i]) > sizeFor(children[j])
		}
	}
	sort.SliceStable(children, less)
	return children
}

func sizeFor(node *domain.Node) int64 {
	if node.Type == domain.NodeDir {
		return node.AccumBytes
	}
	return node.SizeBytes
}

func isHiddenName(name string) bool {
	return strings.HasPrefix(name, ".")
}

func (appState *State) nodeMatches(node *domain.Node) bool {
	if node == nil {
		return false
	}
	if appState.SearchQuery != "" {
		query := strings.ToLower(appState.SearchQuery)
		if !strings.Contains(strings.ToLower(node.Name), query) {
			return false
		}
	}
	if appState.FilterExt != "" {
		filter := strings.ToLower(strings.TrimPrefix(appState.FilterExt, "."))
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(node.Name), "."))
		if ext != filter {
			return false
		}
	}
	if appState.MinSizeBytes > 0 {
		if sizeFor(node) < appState.MinSizeBytes {
			return false
		}
	}
	return true
}

func (appState *State) dirHasMatch(node *domain.Node) bool {
	if node == nil || node.Type != domain.NodeDir {
		return false
	}
	children := appState.sortedChildren(node)
	for _, child := range children {
		if appState.nodeMatches(child) {
			return true
		}
		if child.Type == domain.NodeDir && appState.dirHasMatch(child) {
			return true
		}
	}
	return false
}

func (appState *State) ClearFilters() {
	appState.SearchQuery = ""
	appState.FilterExt = ""
	appState.MinSizeBytes = 0
}

func (appState *State) ToggleSelection(id string) {
	if id == "" {
		return
	}
	appState.Selected[id] = !appState.Selected[id]
	if !appState.Selected[id] {
		delete(appState.Selected, id)
	}
}

func (appState *State) SelectedPaths() []string {
	paths := make([]string, 0, len(appState.Selected))
	for id := range appState.Selected {
		if node, exists := appState.Tree.Nodes[id]; exists {
			paths = append(paths, node.Path)
		}
	}

	if len(paths) == 0 {
		if node := appState.CurrentNode(); node != nil {
			paths = append(paths, node.Path)
		}
	}

	return paths
}

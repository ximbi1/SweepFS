package services

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"sweepfs/internal/domain"
)

type FSScanner struct {
	mu          sync.RWMutex
	cache       map[string]*domain.Node
	scannedDirs map[string]bool
	progress    chan ScanProgress
	exclusions  map[string]struct{}
	maxDepth    int
	root        string
	cacheEntries map[string]cacheEntry
	cacheLoaded  bool
	cachePath    string
	cacheHiddenFlag bool
}

type fileJob struct {
	path string
}

type fileResult struct {
	path string
	size int64
	err  error
}

func NewFSScanner() *FSScanner {
	cachePath, err := cacheFilePath()
	if err != nil {
		cachePath = ""
	}
	return &FSScanner{
		cache:       make(map[string]*domain.Node),
		scannedDirs: make(map[string]bool),
		exclusions: map[string]struct{}{
			".git":         {},
			"node_modules": {},
			".cache":       {},
		},
		maxDepth: 0,
		cachePath: cachePath,
	}
}

func (scanner *FSScanner) Progress() <-chan ScanProgress {
	scanner.mu.RLock()
	defer scanner.mu.RUnlock()
	return scanner.progress
}

func (scanner *FSScanner) Snapshot() domain.TreeIndex {
	scanner.mu.RLock()
	defer scanner.mu.RUnlock()

	copyMap := make(map[string]*domain.Node, len(scanner.cache))
	for id, node := range scanner.cache {
		clone := *node
		if node.ChildrenIDs != nil {
			clone.ChildrenIDs = append([]string{}, node.ChildrenIDs...)
		}
		copyMap[id] = &clone
	}

	rootID := scanner.root
	if rootID == "" {
		for id, node := range copyMap {
			if node.ParentID == "" {
				rootID = id
				break
			}
		}
	}

	return domain.TreeIndex{
		Nodes:  copyMap,
		RootID: rootID,
	}
}

func (scanner *FSScanner) Invalidate(path string) {
	root := cleanPath(path)

	scanner.mu.Lock()
	defer scanner.mu.Unlock()

	for key := range scanner.cache {
		if isWithin(root, key) {
			delete(scanner.cache, key)
		}
	}
	for key := range scanner.scannedDirs {
		if isWithin(root, key) {
			delete(scanner.scannedDirs, key)
		}
	}
}

func (scanner *FSScanner) Scan(ctx context.Context, req ScanRequest) (ScanResult, error) {
	start := time.Now()
	root := cleanPath(req.RootPath)
	if err := scanner.loadCache(); err != nil {
		progressNonBlocking(scanner.progress, ScanProgress{Path: root, ErrMessage: err.Error()})
	}
	progress := make(chan ScanProgress, 64)
	if err := scanner.setProgress(progress); err != nil {
		return ScanResult{}, err
	}
	defer close(progress)

	if scanner.canReuseRoot(root, req.ShowHidden) {
		nodes := scanner.cachedTree(root)
		scanner.replaceCache(root, nodes)
		progressNonBlocking(progress, ScanProgress{Path: root, Scanned: 0, Completed: true})
		return ScanResult{RootPath: root, Duration: time.Since(start)}, nil
	}

	if scanner.isCached(root) {
		scanner.mu.Lock()
		scanner.root = root
		scanner.mu.Unlock()
		progressNonBlocking(progress, ScanProgress{Path: root, Scanned: 0, Completed: true})
		return ScanResult{RootPath: root, Duration: time.Since(start)}, nil
	}

	nodes := make(map[string]*domain.Node)
	rootNode := &domain.Node{
		ID:      root,
		Name:    filepath.Base(root),
		Path:    root,
		Type:    domain.NodeDir,
		Scanned: true,
	}
	if rootNode.Name == "." || rootNode.Name == string(filepath.Separator) {
		rootNode.Name = root
	}
	rootNode.ParentID = ""
	nodes[root] = rootNode

	workerCount := maxInt(2, runtime.NumCPU())
	jobs := make(chan fileJob, workerCount*8)
	results := make(chan fileResult, workerCount*8)
	var wg sync.WaitGroup
	var nodesMu sync.Mutex
	resultsDone := make(chan struct{})
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go worker(ctx, jobs, results, &wg)
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	go func() {
		defer close(resultsDone)
		var processed int64
		for result := range results {
			processed++
			nodesMu.Lock()
			node, ok := nodes[result.path]
			if ok && result.err == nil {
				node.SizeBytes = result.size
				node.AccumBytes = result.size
			}
			nodesMu.Unlock()
			if processed%200 == 0 {
				progressNonBlocking(progress, ScanProgress{Path: root, Scanned: processed, Current: result.path})
			}
		}
	}()

	var scannedCount int64
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if isPermissionErr(err) {
				progressNonBlocking(progress, ScanProgress{Path: path, Scanned: scannedCount, ErrMessage: err.Error()})
				return nil
			}
			return err
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if path != root {
			if !req.ShowHidden && isHidden(entry.Name()) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !req.ShowHidden && scanner.isExcluded(entry.Name()) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if scanner.maxDepth > 0 && depthFrom(root, path) > scanner.maxDepth {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		if entry.IsDir() {
			if scanner.canReuseDir(path, entry, req.ShowHidden) {
				scanner.mergeCachedSubtree(path, nodes, &nodesMu)
				progressNonBlocking(progress, ScanProgress{Path: path, Scanned: scannedCount, Current: path})
				return filepath.SkipDir
			}
			nodesMu.Lock()
			nodes[path] = &domain.Node{
				ID:       path,
				Name:     entry.Name(),
				Path:     path,
				Type:     domain.NodeDir,
				ParentID: parentPath(root, path),
				Scanned:  true,
			}
			nodesMu.Unlock()
		} else {
			nodesMu.Lock()
			nodes[path] = &domain.Node{
				ID:       path,
				Name:     entry.Name(),
				Path:     path,
				Type:     domain.NodeFile,
				ParentID: parentPath(root, path),
			}
			nodesMu.Unlock()
			jobs <- fileJob{path: path}
		}

		scannedCount++
		if scannedCount%50 == 0 {
			progressNonBlocking(progress, ScanProgress{Path: path, Scanned: scannedCount, Current: path})
		}

		return nil
	})
	close(jobs)
	<-resultsDone

	if walkErr != nil {
		return ScanResult{RootPath: root, Duration: time.Since(start)}, walkErr
	}

	nodesMu.Lock()
	applyHierarchy(nodes)
	applyAccumulation(nodes)
	applyFileCounts(nodes)
	applyDirCounts(nodes)
	nodesMu.Unlock()

	scanner.replaceCache(root, nodes)
	scanner.saveCache(nodes, req.ShowHidden)
	progress <- ScanProgress{Path: root, Scanned: scannedCount, Completed: true}

	return ScanResult{RootPath: root, Duration: time.Since(start)}, nil
}

func worker(ctx context.Context, jobs <-chan fileJob, results chan<- fileResult, wg *sync.WaitGroup) {
	defer wg.Done()
	for job := range jobs {
		if ctx.Err() != nil {
			return
		}
		info, err := os.Lstat(job.path)
		if err != nil {
			results <- fileResult{path: job.path, err: err}
			continue
		}
		results <- fileResult{path: job.path, size: info.Size()}
	}
}

func (scanner *FSScanner) setProgress(progress chan ScanProgress) error {
	scanner.mu.Lock()
	defer scanner.mu.Unlock()
	scanner.progress = progress
	return nil
}

func (scanner *FSScanner) isCached(root string) bool {
	scanner.mu.RLock()
	defer scanner.mu.RUnlock()
	return scanner.scannedDirs[root]
}

func (scanner *FSScanner) replaceCache(root string, nodes map[string]*domain.Node) {
	scanner.mu.Lock()
	defer scanner.mu.Unlock()
	for key := range scanner.cache {
		if isWithin(root, key) {
			delete(scanner.cache, key)
		}
	}
	for key, node := range nodes {
		scanner.cache[key] = node
		if node.Type == domain.NodeDir {
			scanner.scannedDirs[key] = true
		}
	}
	parent := filepath.Dir(root)
	if parent != root {
		if parentNode, ok := scanner.cache[parent]; ok {
			if !containsID(parentNode.ChildrenIDs, root) {
				parentNode.ChildrenIDs = append(parentNode.ChildrenIDs, root)
				parentNode.ChildCount++
			}
		}
	}
	scanner.root = root
}

func (scanner *FSScanner) isExcluded(name string) bool {
	_, excluded := scanner.exclusions[name]
	return excluded
}

func applyHierarchy(nodes map[string]*domain.Node) {
	for _, node := range nodes {
		if node.ParentID == "" {
			continue
		}
		parent, ok := nodes[node.ParentID]
		if !ok {
			continue
		}
		parent.ChildrenIDs = append(parent.ChildrenIDs, node.ID)
		if node.Type == domain.NodeDir {
			parent.ChildCount++
		}
	}
}

func applyAccumulation(nodes map[string]*domain.Node) {
	paths := make([]string, 0, len(nodes))
	for path := range nodes {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		return depth(paths[i]) > depth(paths[j])
	})

	for _, path := range paths {
		node := nodes[path]
		if node.Type == domain.NodeFile {
			if node.AccumBytes == 0 {
				node.AccumBytes = node.SizeBytes
			}
			continue
		}
		var total int64
		for _, childID := range node.ChildrenIDs {
			if child, ok := nodes[childID]; ok {
				total += child.AccumBytes
			}
		}
		node.AccumBytes = total
	}
}

func applyFileCounts(nodes map[string]*domain.Node) {
	paths := make([]string, 0, len(nodes))
	for path := range nodes {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		return depth(paths[i]) > depth(paths[j])
	})

	for _, path := range paths {
		node := nodes[path]
		if node.Type == domain.NodeFile {
			node.FileCount = 1
			continue
		}
		count := 0
		for _, childID := range node.ChildrenIDs {
			if child, ok := nodes[childID]; ok {
				count += child.FileCount
			}
		}
		node.FileCount = count
	}
}

func applyDirCounts(nodes map[string]*domain.Node) {
	paths := make([]string, 0, len(nodes))
	for path := range nodes {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		return depth(paths[i]) > depth(paths[j])
	})

	for _, path := range paths {
		node := nodes[path]
		if node.Type == domain.NodeFile {
			node.DirCount = 0
			continue
		}
		count := 0
		for _, childID := range node.ChildrenIDs {
			if child, ok := nodes[childID]; ok {
				if child.Type == domain.NodeDir {
					count++
				}
				count += child.DirCount
			}
		}
		node.DirCount = count
	}
}

func progressNonBlocking(ch chan<- ScanProgress, msg ScanProgress) {
	select {
	case ch <- msg:
	default:
	}
}

func isHidden(name string) bool {
	return strings.HasPrefix(name, ".")
}

func isWithin(root, path string) bool {
	if root == path {
		return true
	}
	rootWithSep := root + string(filepath.Separator)
	return strings.HasPrefix(path, rootWithSep)
}

func cleanPath(path string) string {
	if path == "" {
		return path
	}
	clean := filepath.Clean(path)
	abs, err := filepath.Abs(clean)
	if err != nil {
		return clean
	}
	return abs
}

func parentPath(root, path string) string {
	if path == root {
		return ""
	}
	return filepath.Dir(path)
}

func depth(path string) int {
	return strings.Count(filepath.Clean(path), string(filepath.Separator))
}

func depthFrom(root, path string) int {
	rootDepth := depth(root)
	pathDepth := depth(path)
	return pathDepth - rootDepth
}

func isPermissionErr(err error) bool {
	return errors.Is(err, os.ErrPermission)
}

func containsID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

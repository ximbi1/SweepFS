package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"sweepfs/internal/domain"
)

const cacheVersion = 1
const maxCacheBytes = 50 * 1024 * 1024

type cacheFile struct {
	Version    int                 `json:"version"`
	ShowHidden bool                `json:"showHidden"`
	Entries    map[string]cacheEntry `json:"entries"`
}

type cacheEntry struct {
	Path       string     `json:"path"`
	Name       string     `json:"name"`
	Type       domain.NodeType `json:"type"`
	ModTime    int64      `json:"modTime"`
	SizeBytes  int64      `json:"sizeBytes"`
	AccumBytes int64      `json:"accumBytes"`
	FileCount  int        `json:"fileCount"`
	DirCount   int        `json:"dirCount"`
	ChildCount int        `json:"childCount"`
	Children   []string   `json:"children"`
	ParentID   string     `json:"parentId"`
}

func cacheFilePath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "sweepfs", "cache.json"), nil
}

func (scanner *FSScanner) loadCache() error {
	scanner.mu.Lock()
	defer scanner.mu.Unlock()
	if scanner.cacheLoaded || scanner.cachePath == "" {
		scanner.cacheLoaded = true
		return nil
	}
	info, err := os.Stat(scanner.cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			scanner.cacheLoaded = true
			return nil
		}
		return err
	}
	if info.Size() > maxCacheBytes {
		return fmt.Errorf("cache too large")
	}
	data, err := os.ReadFile(scanner.cachePath)
	if err != nil {
		return err
	}
	var cached cacheFile
	if err := json.Unmarshal(data, &cached); err != nil {
		return err
	}
	if cached.Version != cacheVersion {
		return nil
	}
	scanner.cacheEntries = cached.Entries
	scanner.cacheHiddenFlag = cached.ShowHidden
	scanner.cacheLoaded = true
	return nil
}

func (scanner *FSScanner) saveCache(nodes map[string]*domain.Node, showHidden bool) {
	if scanner.cachePath == "" {
		return
	}
	entries := make(map[string]cacheEntry, len(nodes))
	for path, node := range nodes {
		entries[path] = cacheEntry{
			Path:       node.Path,
			Name:       node.Name,
			Type:       node.Type,
			ModTime:    node.ModTime.UnixNano(),
			SizeBytes:  node.SizeBytes,
			AccumBytes: node.AccumBytes,
			FileCount:  node.FileCount,
			DirCount:   node.DirCount,
			ChildCount: node.ChildCount,
			Children:   append([]string{}, node.ChildrenIDs...),
			ParentID:   node.ParentID,
		}
	}
	file := cacheFile{Version: cacheVersion, ShowHidden: showHidden, Entries: entries}
	data, err := json.Marshal(file)
	if err != nil || len(data) > maxCacheBytes {
		return
	}
	_ = os.MkdirAll(filepath.Dir(scanner.cachePath), 0o755)
	_ = os.WriteFile(scanner.cachePath, data, 0o600)
}

func (scanner *FSScanner) canReuseRoot(path string, showHidden bool) bool {
	scanner.mu.RLock()
	entries := scanner.cacheEntries
	scanner.mu.RUnlock()
	if entries == nil {
		return false
	}
	entry, ok := entries[path]
	if !ok {
		return false
	}
	if entry.Type != domain.NodeDir {
		return false
	}
	if !scanner.cacheShowHidden(showHidden) {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return entry.ModTime == info.ModTime().UnixNano()
}

func (scanner *FSScanner) canReuseDir(path string, entry os.DirEntry, showHidden bool) bool {
	scanner.mu.RLock()
	entries := scanner.cacheEntries
	scanner.mu.RUnlock()
	if entries == nil {
		return false
	}
	if !scanner.cacheShowHidden(showHidden) {
		return false
	}
	info, err := entry.Info()
	if err != nil {
		return false
	}
	cached, ok := entries[path]
	if !ok || cached.Type != domain.NodeDir {
		return false
	}
	return cached.ModTime == info.ModTime().UnixNano()
}

func (scanner *FSScanner) cacheShowHidden(showHidden bool) bool {
	if scanner.cacheEntries == nil {
		return false
	}
	return scanner.cacheHiddenFlag == showHidden
}

func (scanner *FSScanner) cachedTree(root string) map[string]*domain.Node {
	entries := scanner.cacheEntries
	if entries == nil {
		return map[string]*domain.Node{}
	}
	nodes := make(map[string]*domain.Node, len(entries))
	for path, entry := range entries {
		if !hasPathPrefix(root, path) {
			continue
		}
		nodes[path] = entry.toNode()
	}
	return nodes
}

func (scanner *FSScanner) mergeCachedSubtree(root string, nodes map[string]*domain.Node, mu *sync.Mutex) {
	entries := scanner.cacheEntries
	if entries == nil {
		return
	}
	mu.Lock()
	for path, entry := range entries {
		if hasPathPrefix(root, path) {
			nodes[path] = entry.toNode()
		}
	}
	mu.Unlock()
}

func (entry cacheEntry) toNode() *domain.Node {
	return &domain.Node{
		ID:          entry.Path,
		Name:        entry.Name,
		Path:        entry.Path,
		Type:        entry.Type,
		SizeBytes:   entry.SizeBytes,
		AccumBytes:  entry.AccumBytes,
		ModTime:     timeFrom(entry.ModTime),
		ParentID:    entry.ParentID,
		ChildrenIDs: append([]string{}, entry.Children...),
		ChildCount:  entry.ChildCount,
		FileCount:   entry.FileCount,
		DirCount:    entry.DirCount,
		Scanned:     entry.Type == domain.NodeDir,
	}
}

func timeFrom(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(0, value)
}

func hasPathPrefix(root, path string) bool {
	if root == path {
		return true
	}
	rootWithSep := root + string(filepath.Separator)
	return strings.HasPrefix(path, rootWithSep)
}

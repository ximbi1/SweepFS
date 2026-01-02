package services

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type FSActions struct {
	mu       sync.RWMutex
	progress chan ActionProgress
}

func NewFSActions() *FSActions {
	return &FSActions{}
}

func (actions *FSActions) ActionProgress() <-chan ActionProgress {
	actions.mu.RLock()
	defer actions.mu.RUnlock()
	return actions.progress
}

func (actions *FSActions) Preview(ctx context.Context, req ActionRequest) (ActionPreview, error) {
	paths, err := normalizePaths(req.SourcePaths)
	if err != nil {
		return ActionPreview{}, err
	}
	if err := validateRequest(req, paths); err != nil {
		return ActionPreview{}, err
	}

	preview := ActionPreview{
		Type:        req.Type,
		Sources:     paths,
		Destination: req.Destination,
		Samples:     []string{},
	}

	for _, path := range paths {
		select {
		case <-ctx.Done():
			return ActionPreview{}, ctx.Err()
		default:
		}
		info, err := os.Lstat(path)
		if err != nil {
			preview.Warnings = append(preview.Warnings, err.Error())
			continue
		}
		if info.IsDir() {
			preview.TotalDirs++
			walkErr := filepath.WalkDir(path, func(child string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					preview.Warnings = append(preview.Warnings, walkErr.Error())
					return nil
				}
				if entry.IsDir() {
					if child != path {
						preview.TotalDirs++
					}
					return nil
				}
				preview.TotalFiles++
				if len(preview.Samples) < 5 {
					preview.Samples = append(preview.Samples, child)
				}
				fileInfo, err := entry.Info()
				if err == nil {
					preview.TotalBytes += fileInfo.Size()
				}
				return nil
			})
			if walkErr != nil && !errors.Is(walkErr, context.Canceled) {
				preview.Warnings = append(preview.Warnings, walkErr.Error())
			}
		} else {
			preview.TotalFiles++
			preview.TotalBytes += info.Size()
			if len(preview.Samples) < 5 {
				preview.Samples = append(preview.Samples, path)
			}
		}
	}

	return preview, nil
}

func (actions *FSActions) Execute(ctx context.Context, req ActionRequest) (ActionResult, error) {
	start := time.Now()
	paths, err := normalizePaths(req.SourcePaths)
	if err != nil {
		return ActionResult{Type: req.Type}, err
	}
	if err := validateRequest(req, paths); err != nil {
		return ActionResult{Type: req.Type}, err
	}
	if err := requireConfirmation(req, paths); err != nil {
		return ActionResult{Type: req.Type}, err
	}

	progress := make(chan ActionProgress, 64)
	actions.setProgress(progress)
	defer close(progress)

	result := ActionResult{Type: req.Type}

	switch req.Type {
	case ActionDelete:
		result = actions.deletePaths(ctx, progress, paths)
	case ActionMove:
		result = actions.movePaths(ctx, progress, paths, req.Destination)
	case ActionCopy:
		result = actions.copyPaths(ctx, progress, paths, req.Destination)
	case ActionBackup:
		result = actions.backupPaths(ctx, progress, paths, req.Destination)
	default:
		return ActionResult{Type: req.Type}, fmt.Errorf("unsupported action")
	}

	result.Duration = time.Since(start)
	progress <- ActionProgress{Type: req.Type, Completed: true, Processed: result.SuccessCount + result.FailureCount}
	return result, nil
}

func (actions *FSActions) setProgress(progress chan ActionProgress) {
	actions.mu.Lock()
	defer actions.mu.Unlock()
	actions.progress = progress
}

func (actions *FSActions) deletePaths(ctx context.Context, progress chan<- ActionProgress, paths []string) ActionResult {
	result := ActionResult{Type: ActionDelete}
	for _, path := range paths {
		if ctx.Err() != nil {
			result.Message = "delete cancelled"
			return result
		}
		info, err := os.Lstat(path)
		if err != nil {
			result.FailureCount++
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		if info.IsDir() {
			if err := deleteDirectory(ctx, progress, path, &result); err != nil {
				result.Errors = append(result.Errors, err.Error())
			}
			continue
		}
		if err := os.Remove(path); err != nil {
			result.FailureCount++
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		result.SuccessCount++
		actionProgressNonBlocking(progress, ActionProgress{Type: ActionDelete, Current: path, Processed: result.SuccessCount + result.FailureCount})
	}
	result.Message = "delete complete"
	return result
}

func (actions *FSActions) movePaths(ctx context.Context, progress chan<- ActionProgress, paths []string, destination string) ActionResult {
	result := ActionResult{Type: ActionMove}
	resolvedDest, destDir, err := resolveDestination(destination, paths)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		result.Message = "move failed"
		return result
	}

	for _, source := range paths {
		if ctx.Err() != nil {
			result.Message = "move cancelled"
			return result
		}
		target := resolvedDest
		if destDir {
			target = filepath.Join(resolvedDest, filepath.Base(source))
		}
		if exists(target) {
			result.FailureCount++
			result.Errors = append(result.Errors, fmt.Sprintf("target exists: %s", target))
			continue
		}
		if err := os.Rename(source, target); err != nil {
			if !errors.Is(err, syscall.EXDEV) {
				result.FailureCount++
				result.Errors = append(result.Errors, err.Error())
				continue
			}
			if err := copyPath(ctx, progress, source, target, ActionMove); err != nil {
				result.FailureCount++
				result.Errors = append(result.Errors, err.Error())
				continue
			}
			_ = actions.deletePaths(ctx, progress, []string{source})
			result.SuccessCount++
			continue
		}
		result.SuccessCount++
		actionProgressNonBlocking(progress, ActionProgress{Type: ActionMove, Current: target, Processed: result.SuccessCount + result.FailureCount})
	}
	result.Message = "move complete"
	return result
}

func (actions *FSActions) copyPaths(ctx context.Context, progress chan<- ActionProgress, paths []string, destination string) ActionResult {
	result := ActionResult{Type: ActionCopy}
	resolvedDest, destDir, err := resolveDestination(destination, paths)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		result.Message = "copy failed"
		return result
	}

	for _, source := range paths {
		if ctx.Err() != nil {
			result.Message = "copy cancelled"
			return result
		}
		target := resolvedDest
		if destDir {
			target = filepath.Join(resolvedDest, filepath.Base(source))
		}
		if exists(target) {
			result.FailureCount++
			result.Errors = append(result.Errors, fmt.Sprintf("target exists: %s", target))
			continue
		}
		if err := copyPath(ctx, progress, source, target, ActionCopy); err != nil {
			result.FailureCount++
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		result.SuccessCount++
		actionProgressNonBlocking(progress, ActionProgress{Type: ActionCopy, Current: target, Processed: result.SuccessCount + result.FailureCount})
	}
	result.Message = "copy complete"
	return result
}

func (actions *FSActions) backupPaths(ctx context.Context, progress chan<- ActionProgress, paths []string, destination string) ActionResult {
	result := ActionResult{Type: ActionBackup}
	if destination == "" {
		result.Errors = append(result.Errors, "destination required")
		result.Message = "backup failed"
		return result
	}
	if strings.HasSuffix(destination, ".tar.gz") {
		return actions.backupCompressed(ctx, progress, paths, destination)
	}
	return actions.backupCopy(ctx, progress, paths, destination)
}

func validateRequest(req ActionRequest, paths []string) error {
	if len(paths) == 0 {
		return fmt.Errorf("no sources provided")
	}
	if (req.Type == ActionMove || req.Type == ActionCopy || req.Type == ActionBackup) && req.Destination == "" {
		return fmt.Errorf("destination required")
	}
	if req.SafeMode && req.Type == ActionDelete {
		for _, path := range paths {
			if isCriticalPath(path) {
				return fmt.Errorf("blocked critical path: %s", path)
			}
		}
	}
	return nil
}

func requireConfirmation(req ActionRequest, paths []string) error {
	if req.Type != ActionDelete && req.Type != ActionMove {
		return nil
	}
	if req.ConfirmToken == "confirm" {
		return nil
	}
	if req.Type == ActionDelete {
		for _, path := range paths {
			info, err := os.Lstat(path)
			if err == nil && info.IsDir() {
				if req.ConfirmToken != "confirm-recursive" {
					return fmt.Errorf("recursive delete requires confirmation")
				}
			}
		}
		if req.ConfirmToken == "confirm-recursive" {
			return nil
		}
		return fmt.Errorf("delete confirmation required")
	}
	return fmt.Errorf("confirmation required")
}

func normalizePaths(paths []string) ([]string, error) {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		clean := filepath.Clean(abs)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		result = append(result, clean)
	}
	return result, nil
}

func isCriticalPath(path string) bool {
	path = filepath.Clean(path)
	critical := []string{"/", "/etc", "/usr", "/var"}
	if home, err := os.UserHomeDir(); err == nil {
		critical = append(critical, home)
	}
	for _, root := range critical {
		root = filepath.Clean(root)
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func resolveDestination(destination string, sources []string) (string, bool, error) {
	if destination == "" {
		return "", false, fmt.Errorf("destination required")
	}
	abs, err := filepath.Abs(destination)
	if err != nil {
		return "", false, err
	}
	info, err := os.Stat(abs)
	if err == nil && info.IsDir() {
		if len(sources) > 1 {
			return abs, true, nil
		}
		return abs, true, nil
	}
	if len(sources) > 1 {
		return "", false, fmt.Errorf("destination must be a directory for multiple sources")
	}
	return abs, false, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyPath(ctx context.Context, progress chan<- ActionProgress, source, target string, actionType ActionType) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDirectory(ctx, progress, source, target, info.Mode(), actionType)
	}
	return copyFile(ctx, progress, source, target, info, actionType)
}

func copyDirectory(ctx context.Context, progress chan<- ActionProgress, source, target string, mode os.FileMode, actionType ActionType) error {
	if err := os.MkdirAll(target, mode.Perm()); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		outPath := filepath.Join(target, rel)
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(outPath, info.Mode().Perm())
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if err := copyFile(ctx, progress, path, outPath, info, actionType); err != nil {
			return err
		}
		return nil
	})
}

func copyFile(ctx context.Context, progress chan<- ActionProgress, source, target string, info os.FileInfo, actionType ActionType) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	_ = os.Chtimes(target, time.Now(), info.ModTime())
	actionProgressNonBlocking(progress, ActionProgress{Type: actionType, Current: target})
	return nil
}

func deleteDirectory(ctx context.Context, progress chan<- ActionProgress, path string, result *ActionResult) error {
	dirs := []string{}
	walkErr := filepath.WalkDir(path, func(child string, entry fs.DirEntry, err error) error {
		if err != nil {
			result.FailureCount++
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if entry.IsDir() {
			dirs = append(dirs, child)
			return nil
		}
		if err := os.Remove(child); err != nil {
			result.FailureCount++
			result.Errors = append(result.Errors, err.Error())
			return nil
		}
		result.SuccessCount++
		actionProgressNonBlocking(progress, ActionProgress{Type: ActionDelete, Current: child, Processed: result.SuccessCount + result.FailureCount})
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, context.Canceled) {
		return walkErr
	}
	for index := len(dirs) - 1; index >= 0; index-- {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := os.Remove(dirs[index]); err != nil {
			result.FailureCount++
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		result.SuccessCount++
		actionProgressNonBlocking(progress, ActionProgress{Type: ActionDelete, Current: dirs[index], Processed: result.SuccessCount + result.FailureCount})
	}
	return nil
}

func (actions *FSActions) backupCopy(ctx context.Context, progress chan<- ActionProgress, paths []string, destination string) ActionResult {
	result := ActionResult{Type: ActionBackup}
	backupRoot, err := filepath.Abs(destination)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		result.Message = "backup failed"
		return result
	}
	if exists(backupRoot) {
		result.Errors = append(result.Errors, "backup destination exists")
		result.Message = "backup failed"
		return result
	}
	if err := os.MkdirAll(backupRoot, 0o755); err != nil {
		result.Errors = append(result.Errors, err.Error())
		result.Message = "backup failed"
		return result
	}
	for _, source := range paths {
		if ctx.Err() != nil {
			result.Message = "backup cancelled"
			return result
		}
		target := filepath.Join(backupRoot, filepath.Base(source))
		if err := copyPath(ctx, progress, source, target, ActionBackup); err != nil {
			result.FailureCount++
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		result.SuccessCount++
		actionProgressNonBlocking(progress, ActionProgress{Type: ActionBackup, Current: target, Processed: result.SuccessCount + result.FailureCount})
	}
	result.Message = fmt.Sprintf("backup complete: %s", backupRoot)
	return result
}

func (actions *FSActions) backupCompressed(ctx context.Context, progress chan<- ActionProgress, paths []string, destination string) ActionResult {
	result := ActionResult{Type: ActionBackup}
	archivePath, err := filepath.Abs(destination)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		result.Message = "backup failed"
		return result
	}
	if exists(archivePath) {
		result.Errors = append(result.Errors, "backup archive exists")
		result.Message = "backup failed"
		return result
	}
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		result.Errors = append(result.Errors, err.Error())
		result.Message = "backup failed"
		return result
	}
	file, err := os.Create(archivePath)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		result.Message = "backup failed"
		return result
	}
	defer file.Close()
	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	for _, source := range paths {
		if ctx.Err() != nil {
			result.Message = "backup cancelled"
			return result
		}
		base := filepath.Base(source)
		if err := addToArchive(ctx, tarWriter, source, base, progress, &result); err != nil {
			result.Errors = append(result.Errors, err.Error())
			result.FailureCount++
			continue
		}
	}

	result.Message = fmt.Sprintf("backup complete: %s", archivePath)
	return result
}

func addToArchive(ctx context.Context, writer *tar.Writer, source, base string, progress chan<- ActionProgress, result *ActionResult) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			result.FailureCount++
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			result.FailureCount++
			return nil
		}
		name := filepath.Join(base, rel)
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			result.FailureCount++
			return nil
		}
		header.Name = name
		if info.IsDir() && !strings.HasSuffix(header.Name, "/") {
			header.Name += "/"
		}
		if err := writer.WriteHeader(header); err != nil {
			result.Errors = append(result.Errors, err.Error())
			result.FailureCount++
			return nil
		}
		if info.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			result.FailureCount++
			return nil
		}
		_, err = io.Copy(writer, file)
		file.Close()
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			result.FailureCount++
			return nil
		}
		result.SuccessCount++
		actionProgressNonBlocking(progress, ActionProgress{Type: ActionBackup, Current: path, Processed: result.SuccessCount + result.FailureCount})
		return nil
	})
}

func actionProgressNonBlocking(ch chan<- ActionProgress, msg ActionProgress) {
	select {
	case ch <- msg:
	default:
	}
}

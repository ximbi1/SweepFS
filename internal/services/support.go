package services

import (
	"context"

	"sweepfs/internal/domain"
)

type ScanProgress struct {
	Path       string
	Scanned    int64
	Completed  bool
	ErrMessage string
	Current    string
}

type ActionPreview struct {
	Type        ActionType
	Sources     []string
	Destination string
	TotalFiles  int
	TotalDirs   int
	TotalBytes  int64
	Samples     []string
	Warnings    []string
}

type ActionProgress struct {
	Type       ActionType
	Current    string
	Processed  int
	Total      int
	Completed  bool
	ErrMessage string
}

type ProgressProvider interface {
	Progress() <-chan ScanProgress
}

type ActionPreviewer interface {
	Preview(ctx context.Context, req ActionRequest) (ActionPreview, error)
}

type ActionProgressProvider interface {
	ActionProgress() <-chan ActionProgress
}

type SnapshotProvider interface {
	Snapshot() domain.TreeIndex
}

type Invalidator interface {
	Invalidate(path string)
}

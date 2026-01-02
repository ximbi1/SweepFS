package services

import (
	"context"
	"fmt"
	"time"
)

type MockScanner struct{}

func NewMockScanner() *MockScanner {
	return &MockScanner{}
}

func (scanner *MockScanner) Scan(ctx context.Context, req ScanRequest) (ScanResult, error) {
	start := time.Now()
	select {
	case <-ctx.Done():
		return ScanResult{}, ctx.Err()
	case <-time.After(350 * time.Millisecond):
	}

	return ScanResult{
		RootPath: req.RootPath,
		Duration: time.Since(start),
	}, nil
}

type MockActions struct{}

func NewMockActions() *MockActions {
	return &MockActions{}
}

func (actions *MockActions) Execute(ctx context.Context, req ActionRequest) (ActionResult, error) {
	start := time.Now()
	select {
	case <-ctx.Done():
		return ActionResult{}, ctx.Err()
	case <-time.After(450 * time.Millisecond):
	}

	count := len(req.SourcePaths)
	if count == 0 {
		count = 1
	}

	return ActionResult{
		Type:         req.Type,
		SuccessCount: count,
		FailureCount: 0,
		Duration:     time.Since(start),
		Message:      fmt.Sprintf("%s completed", req.Type),
		Errors:       nil,
		Skipped:      0,
	}, nil
}

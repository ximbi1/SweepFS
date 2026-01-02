package services

import "context"

type Scanner interface {
	Scan(ctx context.Context, req ScanRequest) (ScanResult, error)
}

type Actions interface {
	Execute(ctx context.Context, req ActionRequest) (ActionResult, error)
}

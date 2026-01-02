package ui

import "sweepfs/internal/services"

type scanResultMsg struct {
	result services.ScanResult
	err    error
}

type scanProgressMsg struct {
	progress services.ScanProgress
}

type actionResultMsg struct {
	result services.ActionResult
	err    error
}

type actionPreviewMsg struct {
	preview services.ActionPreview
	err     error
}

type actionProgressMsg struct {
	progress services.ActionProgress
}

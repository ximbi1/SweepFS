package services

import "time"

type ScanResult struct {
	RootPath string
	Duration time.Duration
}

type ActionResult struct {
	Type         ActionType
	SuccessCount int
	FailureCount int
	Duration     time.Duration
	Message      string
	Errors       []string
	Skipped      int
}

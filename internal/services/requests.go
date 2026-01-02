package services

type ScanRequest struct {
	RootPath   string
	ShowHidden bool
}

type ActionType string

const (
	ActionDelete ActionType = "delete"
	ActionMove   ActionType = "move"
	ActionCopy   ActionType = "copy"
	ActionBackup ActionType = "backup"
)

type ActionRequest struct {
	Type         ActionType
	SourcePaths  []string
	Destination  string
	SafeMode     bool
	ConfirmToken string
}

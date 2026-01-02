package domain

import "time"

type NodeType int

const (
	NodeFile NodeType = iota
	NodeDir
)

type Node struct {
	ID          string
	Name        string
	Path        string
	Type        NodeType
	SizeBytes   int64
	AccumBytes  int64
	ModTime     time.Time
	ParentID    string
	ChildrenIDs []string
	ChildCount  int
	FileCount   int
	DirCount    int
	Scanned     bool
}

type TreeIndex struct {
	Nodes  map[string]*Node
	RootID string
}

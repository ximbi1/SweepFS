package domain

type SortMode string

const (
	SortBySize SortMode = "size"
	SortByName SortMode = "name"
	SortByMod  SortMode = "mod"
)

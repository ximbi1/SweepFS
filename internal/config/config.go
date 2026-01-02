package config

import "sweepfs/internal/domain"

type Config struct {
	Path            string            `json:"path"`
	ShowHidden      bool              `json:"showHidden"`
	SafeMode        bool              `json:"safeMode"`
	SortMode        domain.SortMode   `json:"sortMode"`
	Theme           string            `json:"theme"`
	KeyBindings     map[string]string `json:"keyBindings"`
	LastDestination string            `json:"lastDestination"`
}

type fileConfig struct {
	Path            *string           `json:"path"`
	ShowHidden      *bool             `json:"showHidden"`
	SafeMode        *bool             `json:"safeMode"`
	SortMode        *string           `json:"sortMode"`
	Theme           *string           `json:"theme"`
	KeyBindings     map[string]string `json:"keyBindings"`
	LastDestination *string           `json:"lastDestination"`
}

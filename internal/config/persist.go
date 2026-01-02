package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"sweepfs/internal/domain"
)

const (
	configDirName  = "sweepfs"
	configFileName = "config.json"
)

func DefaultConfig() Config {
	return Config{
		Path:        ".",
		ShowHidden:  false,
		SafeMode:    true,
		SortMode:    domain.SortBySize,
		Theme:       "dark",
		KeyBindings: map[string]string{},
	}
}

func ConfigPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, configDirName, configFileName), nil
}

func LoadConfig() (Config, error) {
	config := DefaultConfig()
	path, err := ConfigPath()
	if err != nil {
		return config, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil
		}
		return config, err
	}
	var stored fileConfig
	if err := json.Unmarshal(data, &stored); err != nil {
		return config, err
	}
	return mergeConfig(config, stored), nil
}

func SaveConfig(config Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func mergeConfig(base Config, stored fileConfig) Config {
	merged := base
	if stored.Path != nil {
		merged.Path = *stored.Path
	}
	if stored.ShowHidden != nil {
		merged.ShowHidden = *stored.ShowHidden
	}
	if stored.SafeMode != nil {
		merged.SafeMode = *stored.SafeMode
	}
	if stored.SortMode != nil {
		merged.SortMode = domainSortMode(*stored.SortMode, base.SortMode)
	}
	if stored.Theme != nil {
		merged.Theme = *stored.Theme
	}
	if stored.KeyBindings != nil {
		merged.KeyBindings = stored.KeyBindings
	}
	if stored.LastDestination != nil {
		merged.LastDestination = *stored.LastDestination
	}
	return merged
}

func domainSortMode(value string, fallback domain.SortMode) domain.SortMode {
	switch domain.SortMode(value) {
	case domain.SortByName, domain.SortByMod, domain.SortBySize:
		return domain.SortMode(value)
	default:
		return fallback
	}
}

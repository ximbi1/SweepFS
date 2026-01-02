package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"sweepfs/internal/config"
	"sweepfs/internal/services"
	"sweepfs/internal/state"
	"sweepfs/internal/ui"
)

func Run() {
	base := config.DefaultConfig()
	loaded, err := config.LoadConfig()
	if err == nil {
		base = loaded
	}
	cfg := config.ParseFlags(base)
	initialState := state.NewState(cfg)
	if err := initialState.LoadListing(cfg.Path); err != nil {
		fmt.Println("SweepFS listing warning:", err)
	}

	scanner := services.NewFSScanner()
	actions := services.NewFSActions()

	model := ui.NewModel(initialState, scanner, actions)
	if err != nil {
		model = model.WithStatus("Config warning: using defaults")
	}
	program := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := program.Run()
	if err != nil {
		fmt.Println("SweepFS error:", err)
		return
	}
	if provider, ok := finalModel.(ui.ConfigProvider); ok {
		if err := config.SaveConfig(provider.ConfigSnapshot()); err != nil {
			fmt.Println("SweepFS config save error:", err)
		}
	}
}

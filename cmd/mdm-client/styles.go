package main

import "charm.land/lipgloss/v2"

// Catppuccin Mocha subset, matching the look of the ledger TUI.
var (
	cPrimary = lipgloss.Color("#CBA6F7")
	cMuted   = lipgloss.Color("#6C7086")
	cGreen   = lipgloss.Color("#A6E3A1")
	cRed     = lipgloss.Color("#F38BA8")
	cBlue    = lipgloss.Color("#89B4FA")
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(cPrimary)
	hintStyle   = lipgloss.NewStyle().Foreground(cMuted)
	errorStyle  = lipgloss.NewStyle().Bold(true).Foreground(cRed)
	okStyle     = lipgloss.NewStyle().Bold(true).Foreground(cGreen)
	selStyle    = lipgloss.NewStyle().Foreground(cBlue)
	cursorStyle = lipgloss.NewStyle().Bold(true).Foreground(cPrimary)
)

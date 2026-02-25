package ui

import "github.com/charmbracelet/lipgloss"

var (
	ColorSuccess = lipgloss.Color("10")  // bright green
	ColorWarning = lipgloss.Color("11")  // bright yellow
	ColorError   = lipgloss.Color("9")   // bright red
	ColorInfo    = lipgloss.Color("12")  // bright blue
	ColorAccent  = lipgloss.Color("14")  // bright cyan  — commands, highlights
	ColorMuted   = lipgloss.Color("240") // dark gray    — hints, secondary text
)

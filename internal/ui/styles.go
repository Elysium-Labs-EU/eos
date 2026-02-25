package ui

import "github.com/charmbracelet/lipgloss"

var (
	TextMuted   = lipgloss.NewStyle().Faint(true)                        // hints, next-step lines
	TextCommand = lipgloss.NewStyle().Bold(true).Foreground(ColorAccent) // eos add <path>
	TextBold    = lipgloss.NewStyle().Bold(true)

	SectionHeader = lipgloss.NewStyle().Bold(true)
	SectionRule   = lipgloss.NewStyle().Faint(true)
	KeyStyle      = lipgloss.NewStyle().Faint(true).Width(20)
	ValueStyle    = lipgloss.NewStyle().PaddingLeft(1)

	LabelSuccess = lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess)
	LabelWarning = lipgloss.NewStyle().Bold(true).Foreground(ColorWarning)
	LabelError   = lipgloss.NewStyle().Bold(true).Foreground(ColorError)
	LabelInfo    = lipgloss.NewStyle().Bold(true).Foreground(ColorInfo)
	LabelStep    = lipgloss.NewStyle().Bold(true).Foreground(ColorAccent) // → arrow

	TableBorderColor = lipgloss.Color("240")
	TableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorAccent).
				Padding(0, 1)

	TableCellStyle = lipgloss.NewStyle().Padding(0, 1)

	TableEvenRowStyle = TableCellStyle.Foreground(lipgloss.Color("252"))
	TableOddRowStyle  = TableCellStyle.Foreground(lipgloss.Color("245"))
	TableMutedStyle   = TableCellStyle.Foreground(ColorMuted).Faint(true)
)

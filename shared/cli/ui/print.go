package ui

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

func Print(msg string) {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Bold(true)
	println(style.Render(msg))
}
func Printf(format string, args ...interface{}) {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Bold(true)
	println(style.Render(fmt.Sprintf(format, args...)))
}

func PrintError(format string, args ...interface{}) {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true)
	println(style.Render(fmt.Sprintf(format, args...)))
}

func Debug(format string, args ...interface{}) {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#b9b9b9")).Bold(true)
	println(style.Render(fmt.Sprintf(format, args...)))
}

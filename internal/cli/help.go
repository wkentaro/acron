package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	headerStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2")) // bold green
	cmdStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")) // bold cyan
	argStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))            // cyan
	commentStyle = lipgloss.NewStyle().Faint(true)                                // dim
	errorStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")) // bold red
	addStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))            // green
	removeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))            // red
	runningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))            // yellow
)

type row struct {
	left  string
	right string
}

func renderHelp(cmd *cobra.Command) string {
	var b strings.Builder

	if desc := cmd.Short; desc != "" {
		fmt.Fprintf(&b, "%s\n\n", desc)
	}

	fmt.Fprintf(&b, "%s %s\n", headerStyle.Render("Usage:"), usageLine(cmd))

	if cmds := visibleCommands(cmd); len(cmds) > 0 {
		b.WriteString("\n")
		section(&b, "Commands:", commandRows(cmds))
	}
	if opts := optionRows(cmd); len(opts) > 0 {
		b.WriteString("\n")
		section(&b, "Options:", opts)
	}
	if ex := exampleRows(cmd.Example); len(ex) > 0 {
		b.WriteString("\n")
		section(&b, "Examples:", ex)
	}
	return b.String()
}

func usageLine(cmd *cobra.Command) string {
	prog := cmdStyle.Render(cmd.CommandPath())
	if cmd.HasAvailableSubCommands() && !cmd.Runnable() {
		return prog + " " + argStyle.Render("<COMMAND>")
	}
	parts := []string{prog}
	if fields := strings.Fields(cmd.Use); len(fields) > 1 {
		parts = append(parts, argStyle.Render(strings.Join(fields[1:], " ")))
	}
	if cmd.Flags().HasAvailableFlags() {
		parts = append(parts, argStyle.Render("[OPTIONS]"))
	}
	return strings.Join(parts, " ")
}

func visibleCommands(cmd *cobra.Command) []*cobra.Command {
	var cmds []*cobra.Command
	for _, c := range cmd.Commands() {
		if c.Hidden || c.Name() == "help" {
			continue
		}
		cmds = append(cmds, c)
	}
	return cmds
}

func commandRows(cmds []*cobra.Command) []row {
	rows := make([]row, 0, len(cmds))
	for _, c := range cmds {
		rows = append(rows, row{left: cmdStyle.Render(c.Name()), right: c.Short})
	}
	return rows
}

func optionRows(cmd *cobra.Command) []row {
	cmd.InitDefaultHelpFlag()
	var rows []row
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		left := "    "
		if f.Shorthand != "" {
			left = cmdStyle.Render("-"+f.Shorthand) + ", "
		}
		left += cmdStyle.Render("--" + f.Name)
		if f.Value.Type() != "bool" {
			left += " " + argStyle.Render("<"+f.Value.Type()+">")
		}
		rows = append(rows, row{left: left, right: f.Usage})
	})
	return rows
}

func exampleRows(example string) []row {
	var rows []row
	for _, line := range strings.Split(example, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		command, comment, found := strings.Cut(line, "#")
		command = strings.TrimSpace(command)
		if !found {
			rows = append(rows, row{left: argStyle.Render(command)})
			continue
		}
		rows = append(rows, row{
			left:  argStyle.Render(command),
			right: commentStyle.Render("# " + strings.TrimSpace(comment)),
		})
	}
	return rows
}

func section(b *strings.Builder, header string, rows []row) {
	b.WriteString(headerStyle.Render(header) + "\n")
	width := 0
	for _, r := range rows {
		if w := lipgloss.Width(r.left); w > width {
			width = w
		}
	}
	for _, r := range rows {
		if r.right == "" {
			fmt.Fprintf(b, "  %s\n", r.left)
			continue
		}
		pad := strings.Repeat(" ", width-lipgloss.Width(r.left))
		fmt.Fprintf(b, "  %s%s  %s\n", r.left, pad, r.right)
	}
}

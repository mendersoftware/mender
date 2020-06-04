package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// HelpPrinterCustom is a function that writes the help output. It is used as
// the default implementation of HelpPrinter, and may be called directly if
// the ExtraInfo field is set on an App.
var HelpPrinterCustom helpPrinterCustom = printHelpCustomNoTemplate

// appHelpTextFmt is the format string for application help text.
// Inspired in AppHelpTemplate from template.go
var appHelpTextFmt = `NAME:
   %s - %s

USAGE:
   [global options] command [command options] [arguments...]

VERSION:
   %s

DESCRIPTION:
   %s

COMMANDS:
%s
GLOBAL OPTIONS:
%s
`

// cmdHelpTextFmt is the format string for command help text.
// Inspired in CommandHelpTemplate from template.go
var cmdHelpTextFmt = `NAME:
   %s - %s

USAGE:
   %s [command options] [options]

OPTIONS:
%s
`

// subCmdHelpFmt is the format string for "command with subcommands" help text.
// Inspired in SubcommandHelpTemplate from template.go
var subCmdHelpFmt = `NAME:
   %s - %s

USAGE:
   %s [command options] [options]

DESCRIPTION:
   %s

COMMANDS:
%s
OPTIONS:
%s
`

// printHelpCustomNoTemplate is the default implementation of HelpPrinterCustom.
// Substitutes printHelpCustom from help.go removing templates support
func printHelpCustomNoTemplate(out io.Writer, template string, data interface{}, _ map[string]interface{}) {
	helpStr := ""
	switch d := data.(type) {
	case *App:
		commandsStr := ""
		for _, cmd := range d.Commands {
			commandsStr = commandsStr + fmt.Sprintf("   %s\t%s\n", cmd.Name, cmd.Usage)
		}
		optionsStr := ""
		for _, flag := range d.Flags {
			optionsStr = optionsStr + fmt.Sprintf("   %s\n", flag)
		}
		// It is not possible to figure out if the data passed is an App or a "command with subcommands" (which
		// uses the same App struct). Hack: look for a unique string in the [otherwise ignored] given template.
		if strings.Contains(template, "VERSION") {
			helpStr = fmt.Sprintf(appHelpTextFmt, d.Name, d.Usage, d.Version, d.Description, commandsStr, optionsStr)
		} else {
			helpStr = fmt.Sprintf(subCmdHelpFmt, d.Name, d.Usage, d.Name, d.Description, commandsStr, optionsStr)
		}
	case *Command:
		optionsStr := ""
		for _, flag := range d.Flags {
			optionsStr = optionsStr + fmt.Sprintf("   %s\n", flag)
		}
		helpStr = fmt.Sprintf(cmdHelpTextFmt, d.Name, d.Usage, d.Name, optionsStr)
	}

	w := tabwriter.NewWriter(out, 1, 8, 2, ' ', 0)
	w.Write([]byte(helpStr))
	w.Flush()
}

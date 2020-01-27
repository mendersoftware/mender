package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	defaultWidth int = 80

	columnFraction = 0.3
	maxColumnWidth = 35

	bufferSize = 1024
)

// HelpPrinter provides an interface for printing the help message.
type HelpPrinter struct {
	buf         *bytes.Buffer
	ctx         *Context
	out         io.Writer
	width       int
	columnWidth int

	// RightMargin and LeftMargin specifies the margins for the Write func.
	RightMargin int
	LeftMargin  int
	cursor      int
	sep         string
}

// NewHelpPrinter creates a help printer initialized with the context ctx.
// Using PrintHelp will create a help prompt based on ctx that will be written
// to out.
func NewHelpPrinter(ctx *Context, out io.Writer) *HelpPrinter {
	var width int
	if f, ok := out.(*os.File); ok {
		ws, err := getTerminalSize(int(f.Fd()))
		if err != nil {
			width = defaultWidth
		} else {
			width = int(ws[0])
		}
	}
	if width < 10 {
		width = defaultWidth
	}
	columnWidth := int(columnFraction * float64(width))
	if columnWidth > maxColumnWidth {
		columnWidth = maxColumnWidth
	}

	return &HelpPrinter{
		ctx:         ctx,
		buf:         &bytes.Buffer{},
		out:         out,
		width:       width,
		columnWidth: columnWidth,

		LeftMargin:  0,
		RightMargin: width,
		sep:         " ",
	}
}

// Write function which makes the HelpPrinter conform with the io.Writer
// interface. The printer attempts to insert newlines at word boundaries and
// satisfy the margin constrains in the HelpPrinter structure.
//     NOTE: The returned length is that of the bytes written to the buffer
//           that includes indentation and inserted newlines.
func (hp *HelpPrinter) Write(p []byte) (int, error) {
	var err error
	var n int
	var N int
	var NumExtraChars int
	var pp []byte
	if hp.RightMargin <= hp.LeftMargin {
		hp.LeftMargin = 0
		hp.RightMargin = defaultWidth
	}
	for N < len(p) {
		pp = p[N:]
		if hp.cursor < hp.LeftMargin {
			n, err = fmt.Fprintf(hp.buf, "%*s",
				hp.LeftMargin-hp.cursor, "")
			hp.cursor += n
			NumExtraChars += n
			if err != nil {
				break
			}
			// Trim white-space characters
			for N < len(p) && p[N] == byte(' ') {
				N++
			}
			continue
		}
		lineSpace := hp.RightMargin - hp.cursor
		if lineSpace > len(pp) {
			lineSpace = len(pp)
		} else if lineSpace <= 0 {
			n, err := fmt.Fprintln(hp.buf)
			if err != nil {
				break
			}
			NumExtraChars += n
			hp.cursor = 0
			continue
		}
		if idx := bytes.Index(pp[:lineSpace], []byte(NewLine)); idx >= 0 {
			idx += len(NewLine)
			n, err = hp.buf.Write(pp[:idx])
			hp.cursor = 0
		} else {
			// Need to split last word
			idx = bytes.LastIndex(pp[:lineSpace], []byte(hp.sep))
			if idx < 0 {
				idx = bytes.Index(pp, []byte(hp.sep))
				if idx < 0 {
					idx = len(pp) - 1
				}
				if lineSpace > idx {
					n, err = hp.buf.Write(pp[:idx+1])
				} else if idx > hp.RightMargin-hp.LeftMargin {
					// Last resort, next word doesn't fit so
					// flush the remainder of the line.
					n, err = hp.buf.Write(pp[:lineSpace])
				} else {
					// Insert newline, reset cursor
					n, err = fmt.Fprintln(hp.buf)
					NumExtraChars += n
					hp.cursor = 0
					if err != nil {
						break
					}
					continue
				}
			} else {
				idx++
				n, err = hp.buf.Write(pp[:idx])
			}
			hp.cursor += n
		}
		N += n
		if err != nil {
			break
		}
	} // for N < len(p)
	return N + NumExtraChars, err
}

func (hp *HelpPrinter) initPrint() ([]*Flag, []*Flag, string) {
	var flags []*Flag
	var execStr string

	if hp.ctx.Command == nil {
		flags = hp.ctx.App.Flags
		execStr = hp.ctx.App.Name
	} else {
		for p := hp.ctx; p != nil; p = p.parent {
			if p.Command == nil {
				flags = append(flags, p.App.Flags...)
			} else {
				execStr = p.Command.Name + " " + execStr
				flags = append(flags, p.Command.Flags...)
				if !p.Command.InheritParentFlags {
					break
				}
			}
		}
		execStr = hp.ctx.App.Name + " " + execStr
	}

	optFlags, reqFlags := getOptionalAndRequired(flags)
	return optFlags, reqFlags, execStr
}

// PrintUsage prints the usage string hinting all available and required flags
// and commands without the usage strings.
func (hp *HelpPrinter) PrintUsage() error {
	optFlags, reqFlags, execStr := hp.initPrint()
	err := hp.writeUsage(execStr, reqFlags, optFlags)
	if err != nil {
		return err
	}
	_, err = hp.buf.WriteTo(hp.out)
	return err
}

// PrintHelp prints a verbose formatted help message with usage strings and
// description. If the flag has a default value, the value is appended to the
// usage string in square brackets.
func (hp *HelpPrinter) PrintHelp() error {
	optFlags, reqFlags, execStr := hp.initPrint()
	err := hp.writeUsage(execStr, reqFlags, optFlags)
	if err != nil {
		return err
	}
	if hp.ctx.Command != nil {
		if hp.ctx.Command.Description != "" {
			hp.LeftMargin = 0
			fmt.Fprintln(hp, NewLine+"Description:")
			hp.LeftMargin = 2
			fmt.Fprintln(hp, hp.ctx.Command.Description)
		}
		if len(hp.ctx.Command.SubCommands) > 0 {
			err = hp.writeCommandSection(hp.ctx.Command.SubCommands)
		}
	} else {
		if hp.ctx.App.Description != "" {
			hp.LeftMargin = 0
			fmt.Fprintln(hp, NewLine+"Description:")
			hp.LeftMargin = 2
			fmt.Fprintln(hp, hp.ctx.App.Description)
		}
		if len(hp.ctx.App.Commands) > 0 {
			err = hp.writeCommandSection(hp.ctx.App.Commands)
		}
	}
	if err != nil {
		return err
	}

	if len(reqFlags) > 0 {
		err = hp.writeFlagSection("Required flags", reqFlags)
		if err != nil {
			return err
		}
	}

	if len(optFlags) > 0 {
		err = hp.writeFlagSection("Optional flags", optFlags)
	}
	hp.buf.WriteTo(hp.out)
	return err
}

func (hp *HelpPrinter) writeCommandSection(commands []*Command) error {
	hp.LeftMargin = 0
	_, err := fmt.Fprintln(hp, NewLine+"Commands:")
	if err != nil {
		return err
	}
	for _, cmd := range commands {
		hp.LeftMargin = 2
		_, err = fmt.Fprint(hp, cmd.Name)
		if err != nil {
			return err
		}
		hp.LeftMargin = hp.columnWidth
		_, err = fmt.Fprintln(hp, cmd.Usage)
		if err != nil {
			return err
		}
	}
	return nil
}

func (hp *HelpPrinter) writeFlagSection(section string, flags []*Flag) error {
	hp.LeftMargin = 0
	_, err := fmt.Fprint(hp, NewLine+section+":"+NewLine)
	if err != nil {
		return err
	}
	for _, flag := range flags {
		char := "/-" + string(flag.Char)
		if flag.Char == rune(0) {
			char = ""
		}
		hp.LeftMargin = 2
		metaVar := flag.MetaVar
		if metaVar == "" {
			if flag.Type != Bool {
				metaVar = "value"
			}
		}

		n, err := fmt.Fprintf(hp, "--%s%s %s  ",
			flag.Name, char, metaVar)
		if err != nil {
			return err
		}
		hp.LeftMargin = hp.columnWidth
		if n > hp.LeftMargin {
			fmt.Fprintln(hp)
		}
		fmt.Fprint(hp, flag.String()+NewLine)
	}

	return nil
}

func (hp *HelpPrinter) writeUsage(
	execStr string,
	required, optional []*Flag,
) error {

	n, err := fmt.Fprintf(hp, "Usage: %s", execStr)
	if err != nil {
		return err
	}
	if n < hp.width {
		hp.LeftMargin = n
	}

	for _, flag := range append(required, optional...) {
		word := "--" + flag.Name
		if flag.Char != rune(0) {
			word = "-" + string(flag.Char)
		}
		if flag.MetaVar == "" {
			if flag.Type != Bool {
				word += " value"
			}
		} else {
			word = fmt.Sprintf("%s %s", word, flag.MetaVar)
		}

		if flag.Required {
			word = " " + word
		} else {
			word = " [" + word + "]"
		}
		if hp.cursor+len(word) > hp.RightMargin {
			word = NewLine + word
		}
		n, err = fmt.Fprint(hp, word)
		if err != nil {
			return err
		}
	}

	// Print commands usage, use curly braces if the commands are required
	// and square brackets otherwise.
	cmdString := " ["
	suffix := "]"
	if hp.ctx.Command != nil {
		if len(hp.ctx.Command.PositionalArguments) > 0 {
			fmt.Fprint(hp, " "+strings.Join(
				hp.ctx.Command.PositionalArguments, " "))
		}
		if len(hp.ctx.Command.SubCommands) > 0 {
			if hp.ctx.Command.Action == nil {
				cmdString = " {"
				suffix = "}"
			}
			if len(hp.ctx.Command.SubCommands) >= 10 {
				cmdString += fmt.Sprintf("command%s%soptions%s",
					suffix, cmdString, suffix)
			} else {
				for _, cmd := range hp.ctx.Command.SubCommands {
					cmdString += cmd.Name + ","
				}
			}
			// Remove trailing comma and replace it with suffix
			cmdString = cmdString[:len(cmdString)-1] + suffix
		}
	} else if len(hp.ctx.App.Commands) > 0 {
		if hp.ctx.App.Action == nil {
			cmdString = " {"
			suffix = "}"
		}
		if len(hp.ctx.App.Commands) >= 10 {
			cmdString += fmt.Sprintf("command%s%soptions%s",
				suffix, cmdString, suffix)
		} else {
			for _, cmd := range hp.ctx.App.Commands {
				cmdString += cmd.Name + ","
			}
		}
		// Remove trailing comma and replace it with suffix
		cmdString = cmdString[:len(cmdString)-1] + suffix
	}
	if len(cmdString) <= 2 {
		cmdString = ""
	}
	hp.sep = ","
	_, err = fmt.Fprintln(hp, cmdString)
	hp.sep = " "

	return err
}

func getOptionalAndRequired(flags []*Flag) ([]*Flag, []*Flag) {
	var optional []*Flag
	var required []*Flag
	var numRequired int
	var i, j int

	for _, flag := range flags {
		if flag.Required {
			numRequired++
		}
	}
	required = make([]*Flag, numRequired)
	optional = make([]*Flag, len(flags)-numRequired)
	for _, flag := range flags {
		if flag.Required {
			required[i] = flag
			i++
		} else {
			optional[j] = flag
			j++
		}
	}

	return optional, required
}

var (
	HelpOption = &Flag{
		Name:  "help",
		Char:  'h',
		Type:  Bool,
		Usage: "Display this help message",
	}
	HelpCommand = &Command{
		Name:                "help",
		Usage:               "Show help for command given as argument",
		PositionalArguments: []string{"<command>"},
		Action:              helpCmd,
	}
)

func helpCmd(ctx *Context) error {
	parent := ctx.parent
	args := ctx.GetPositionals()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr,
			"No help subject given, showing default")
		return parent.PrintHelp()
	} else {
		var subjectCommand *Command
		var commands []*Command
		if parent.Command == nil {
			commands = parent.App.Commands
		} else {
			commands = parent.Command.SubCommands
		}
		for _, cmd := range commands {
			if cmd.Name == args[0] {
				subjectCommand = cmd
				break
			}
		}
		if subjectCommand == nil {
			fmt.Fprintf(os.Stderr,
				"Help subject '%s' unknown%s",
				args[0], NewLine)
		} else {
			subjectContext := &Context{
				App:     ctx.App,
				Command: subjectCommand,
				parent:  parent,
			}
			ctx = subjectContext
		}
	}
	return ctx.PrintHelp()
}

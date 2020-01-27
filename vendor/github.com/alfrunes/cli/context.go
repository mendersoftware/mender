package cli

import (
	"fmt"
	"os"
)

// Context provides an interface to the parsed command and arguments. After
// parsing the command can be identified with the Context.Command.Name and
// the flag values are retrieved by the Context.<FlagType>(<Flag.Name>), where
// FlagType is one of the predefined FlagType constants and Flag.Name is the
// string value of flag; this set of functions also return a boolean describing
// whether the flag was parsed or explicitly set using the Context.Set function.
type Context struct {
	App     *App
	Command *Command

	// parent is the context scope of the parent command
	parent *Context

	positionalArgs []string
	scopeFlags     map[string]*Flag
	parsedFlags    map[string]*Flag
	requiredFlags  map[string]*Flag
	scopeCommands  map[string]*Command
}

// NewContext creates a new context. The app argument is required and can't
// be nil, where as the parent context and command are optionally non-nil. The
// context is initialized from configurations specified in the app. Furthermore,
// the presence of a command argument determines the scope of the context (which
// flags will be reachable from the context).
func NewContext(app *App, parent *Context, cmd *Command) (*Context, error) {
	var flags *[]*Flag
	ctx := &Context{
		App:     app,
		Command: cmd,
		parent:  parent,

		parsedFlags:   make(map[string]*Flag),
		requiredFlags: make(map[string]*Flag),
		scopeFlags:    make(map[string]*Flag),
		scopeCommands: make(map[string]*Command),
	}

	if app == nil {
		return nil, internalError(
			fmt.Errorf("NewContext invalid argument: missing app"))
	}

	if cmd == nil {
		// Root scope
		flags = &ctx.App.Flags
		if !ctx.App.DisableHelpCommand && len(ctx.App.Commands) > 0 {
			ctx.App.Commands = append(ctx.App.Commands, HelpCommand)
			ctx.scopeCommands[HelpCommand.Name] = HelpCommand
		}
		for _, cmd := range ctx.App.Commands {
			if err := cmd.Validate(); err != nil {
				return nil, err
			}
			ctx.scopeCommands[cmd.Name] = cmd
		}
	} else {
		// Command scope
		if !ctx.App.DisableHelpCommand &&
			// Add default help command
			len(ctx.Command.SubCommands) > 0 {
			ctx.Command.SubCommands = append(
				ctx.Command.SubCommands, HelpCommand)
		}

		flags = &cmd.Flags
		if cmd.InheritParentFlags {
			for k, v := range parent.scopeFlags {
				ctx.scopeFlags[k] = v
			}
		}
		for _, subCmd := range cmd.SubCommands {
			if err := cmd.Validate(); err != nil {
				return nil, err
			}
			ctx.scopeCommands[subCmd.Name] = subCmd
		}
	}
	if !ctx.App.DisableHelpOption && !(ctx.Command != nil &&
		(ctx.Command.InheritParentFlags ||
			ctx.Command.Name == "help")) {
		if flags != nil {
			*flags = append(*flags, HelpOption)
		}
		ctx.scopeFlags[HelpOption.Name] = HelpOption
	}

	err := ctx.appendFlags(*flags)
	return ctx, err
}

// GetParent returns the parent context
func (ctx *Context) GetParent() *Context {
	return ctx.parent
}

// GetPositionals returns the positional arguments under the scope of the
// context.
func (ctx *Context) GetPositionals() []string {
	return ctx.positionalArgs
}

// String gets the value of the flag with the given name and returns whether the
// flag is set.
func (ctx *Context) String(name string) (string, bool) {
	var ret string = ""
	var isSet bool = false

	for c := ctx; c != nil; c = c.parent {
		if flag, ok := c.scopeFlags[name]; ok {
			if value, ok := flag.value.(string); ok {
				ret = value
			} else {
				break
			}
			if _, ok := c.parsedFlags[name]; ok {
				isSet = true
				break
			}
		}
	}
	return ret, isSet
}

// Int gets the value of the flag with the given name and returns whether the
// flag is set
func (ctx *Context) Int(name string) (int, bool) {
	var ret int = 0
	var isSet bool = false

	for c := ctx; c != nil; c = c.parent {
		if flag, ok := c.scopeFlags[name]; ok {
			if value, ok := flag.value.(int); ok {
				ret = value
			} else {
				break
			}
			if _, ok := c.parsedFlags[name]; ok {
				isSet = true
				break
			}
		}
	}
	return ret, isSet
}

// Bool gets the value of the flag with the given name and returns whether the
// flag is set.
func (ctx *Context) Bool(name string) (bool, bool) {
	var ret bool = false
	var isSet bool = false

	for c := ctx; c != nil; c = c.parent {
		if flag, ok := c.scopeFlags[name]; ok {
			if value, ok := flag.value.(bool); ok {
				ret = value
			} else {
				break
			}
			if _, ok := c.parsedFlags[name]; ok {
				isSet = true
				break
			}
		}
	}
	return ret, isSet
}

// Int gets the value of the flag with the given name and returns whether the
// flag is set
func (ctx *Context) Float(name string) (float64, bool) {
	var ret float64 = 0
	var isSet bool = false

	for c := ctx; c != nil; c = c.parent {
		if flag, ok := c.scopeFlags[name]; ok {
			if value, ok := flag.value.(float64); ok {
				ret = value
			} else {
				break
			}
			if _, ok := c.parsedFlags[name]; ok {
				isSet = true
				break
			}
		}
	}
	return ret, isSet
}

// Set flag to value as parsed from the command-line.
func (ctx *Context) Set(flag, value string) error {
	var err error
	if flag, ok := ctx.scopeFlags[flag]; ok {
		err = flag.Set(value)
		ctx.parsedFlags[flag.Name] = flag
	} else {
		err = fmt.Errorf("flag not defined")
	}
	return err
}

// Free releases all internal lookup maps for garbage collection, after Free
// is called this context will always return empty value and false on flag
// queries.
func (ctx *Context) Free() {
	var p *Context
	for p = ctx; p != nil; p = p.parent {
		p.parsedFlags = nil
		p.positionalArgs = nil
		p.requiredFlags = nil
		p.scopeCommands = nil
		p.scopeFlags = nil
	}
}

// PrintHelp prints the help prompt of the context's scope (command/app).
func (ctx *Context) PrintHelp() error {
	helpPrinter := NewHelpPrinter(ctx, os.Stderr)
	return helpPrinter.PrintHelp()
}

// PrintUsage prints the usage string given the context's scope (command/app).
func (ctx *Context) PrintUsage() error {
	helpPrinter := NewHelpPrinter(ctx, os.Stderr)
	return helpPrinter.PrintUsage()
}

func (ctx *Context) appendFlags(flags []*Flag) error {
	for _, flag := range flags {
		flag.init()
		if err := flag.Validate(); err != nil {
			return err
		}
		if flag == nil {
			return fmt.Errorf("NewContext: nil flag detected!")
		}
		ctx.scopeFlags[flag.Name] = flag
		if flag.Required {
			ctx.requiredFlags[flag.Name] = flag
		}
		if flag.Char != rune(0) {
			ctx.scopeFlags[string(flag.Char)] = flag
		}
	}
	return nil
}

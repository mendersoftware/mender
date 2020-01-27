// cli is an easy-to-use and fairly lightweight commandline parser inspired by
// python argparse and github.com/urfave/cli.
package cli

import (
	"fmt"
	"os"
	"strings"
)

// internalError is a private error type which is caused by illegal usage of
// the flag package, for example assigning wrong default value type to a flag.
type internalError error

type App struct {
	// Name of the application - will also appear as the usage executable
	// in the help text.
	Name string
	// Description should give a short description of the application.
	Description string

	// Action defines the default action (main application) of the program.
	Action func(ctx *Context) error
	// Flags are the flags accessible at the root scope.
	Flags []*Flag
	// Commands are commands accessible at the root scope.
	Commands []*Command

	// DisableHelpOption disables the default <-h/--help> flag.
	DisableHelpOption bool
	// DisableHelpCommand disable the default <help> command.
	DisableHelpCommand bool
}

// Run starts parsing the command-line arguments passed as args, and executes
// the action corresponding with the sequence of arguments. Any errors during
// parsing triggers the usage to be printed to the terminal.
func (app *App) Run(args []string) error {
	appCtx, err := NewContext(app, nil, nil)
	if err != nil {
		return err
	}
	ctx, err := app.parseArgs(args, appCtx)
	if ctx == nil {
		ctx = appCtx
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: "+err.Error())
		if ctx == nil {
			appCtx.PrintUsage()
		} else {
			ctx.PrintUsage()
		}
		return err
	}
	if hjalp, _ := ctx.Bool("help"); hjalp {
		return ctx.PrintHelp()
	}

	if len(ctx.requiredFlags) > 0 {
		missingFlags := "[ "
		for k, _ := range ctx.requiredFlags {
			missingFlags += "--" + k + " "
		}
		missingFlags += "]"
		err := fmt.Errorf(
			"Error: missing argument(s): %s",
			missingFlags)
		fmt.Fprintln(os.Stderr, err.Error())
		ctx.PrintUsage()
		return err
	}

	if ctx.Command == nil {
		if ctx.App.Action == nil {
			ctx.PrintHelp()
			return nil
		} else {
			return ctx.App.Action(ctx)
		}
	} else if ctx.Command.Action == nil {
		ctx.PrintHelp()
		return nil
	}

	return ctx.Command.Action(ctx)
}

// parseArgs parses all passed arguments and on success returns the context
// of the inner command scope.
func (app *App) parseArgs(args []string, ctx *Context) (*Context, error) {
	var flag *Flag
	var err error

	for i, arg := range args {
		// Flag from last iteration - try to assign arg as value.
		if flag != nil {
			if err = flag.Set(arg); err != nil && flag.Type != Bool {
				return ctx, fmt.Errorf(
					"Error parsing flag %s: %s",
					args[i-1], err.Error())
			}
			flag = nil
			if err == nil {
				continue
			}
		}

		ret, err := parseArg(arg, ctx)
		if err != nil {
			return ctx, err
		}
		switch ret.(type) {
		case *Flag:
			flag = ret.(*Flag)
			if flag.Type == Bool {
				flag.value = true
				ctx.parsedFlags[flag.Name] = flag
			}

		case *Command:
			cmd := ret.(*Command)
			ctx, err = NewContext(app, ctx, cmd)
			if err != nil {
				return nil, err
			}

		case string:
			p := ret.(string)
			if p == "--" {
				ctx.positionalArgs = append(
					ctx.positionalArgs, args[i:]...)
				return ctx, nil
			}
			ctx.positionalArgs = append(ctx.positionalArgs, p)
		}
	}

	if flag != nil && flag.Type != Bool {
		return ctx, fmt.Errorf(
			"The following flag is missing a (%s) value: %s",
			flag.Type, args[len(args)-1])
	}

	return ctx, nil
}

func parseArg(arg string, ctx *Context) (interface{}, error) {
	var ret interface{}

	if arg[:2] == "--" {
		flagKeyVal := strings.SplitN(arg[2:], "=", 2)
		flagAddr, ok := ctx.scopeFlags[flagKeyVal[0]]
		if !ok {
			return nil, fmt.Errorf("unrecognized flag: %s", arg)
		}
		switch len(flagKeyVal) {
		// Flag has the form --flag=value
		case 2:
			if err := flagAddr.Set(flagKeyVal[1]); err != nil {
				return nil, err
			}
			ctx.parsedFlags[flagKeyVal[0]] = flagAddr
			ret = nil

		// Flag has the form --flag [value]
		case 1:
			ret = flagAddr
		}

		delete(ctx.requiredFlags, flagAddr.Name)
		if _, ok := ctx.parsedFlags[flagKeyVal[0]]; ok {
			return nil, fmt.
				Errorf("flag provided more than once: %s",
					flagKeyVal[0])
		}
		return ret, nil

	} else if arg[0] == '-' {
		// Handle short flag (possibly compound)
		if arg == "-" {
			// Treat single hyphen as positional argument
			return arg, nil
		}
		var flag *Flag
		var ok bool
		rawFlags := strings.Split(arg[1:], "")
		lastIdx := len(rawFlags) - 1
		for i, char := range rawFlags {
			flag, ok = ctx.scopeFlags[char]
			if !ok {
				return nil, fmt.Errorf(
					"unrecognized option: %s", char)
			}
			if _, ok = ctx.parsedFlags[flag.Name]; ok {
				return nil, fmt.Errorf(
					"flag provided more than once: " +
						flag.Name)
			}
			ctx.parsedFlags[flag.Name] = flag
			delete(ctx.requiredFlags, flag.Name)
			if i == lastIdx {
				break
			}

			if flag.Type != Bool {
				if i == lastIdx {
					break
				}
				return nil, fmt.Errorf(
					"flag %s (type: %s) cannot be used "+
						"in a compound expression '%s'",
					flag.Char, flag.Type, arg)
			}
			flag.value = true
		}
		return flag, nil
	} else if cmd, ok := ctx.scopeCommands[arg]; ok {
		// Check if arg is a command
		return cmd, nil
	}
	return arg, nil
}

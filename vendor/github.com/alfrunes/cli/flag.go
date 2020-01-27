package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type FlagType uint8

const (
	String FlagType = iota
	Bool
	Int
	Float
)
const unknown FlagType = 0xFF

func (ft FlagType) Equal(value interface{}) bool {
	actualType := getFlagType(value)
	if ft != actualType {
		return false
	}
	return true
}

func (ft FlagType) CastSlice(slice interface{}) ([]interface{}, bool) {
	switch ft {
	case Bool:
		sb, ok := slice.([]bool)
		if ok {
			ret := make([]interface{}, len(sb))
			for i, e := range sb {
				ret[i] = e
			}
			return ret, true
		}
	case Float:
		sf, ok := slice.([]float64)
		if ok {
			ret := make([]interface{}, len(sf))
			for i, e := range sf {
				ret[i] = e
			}
			return ret, true
		}
	case Int:
		si, ok := slice.([]int)
		if ok {
			ret := make([]interface{}, len(si))
			for i, e := range si {
				ret[i] = e
			}
			return ret, true
		}
	case String:
		ss, ok := slice.([]string)
		if ok {
			ret := make([]interface{}, len(ss))
			for i, e := range ss {
				ret[i] = e
			}
			return ret, true
		}
	}
	return nil, false
}

func (ft FlagType) Nil() interface{} {
	switch ft {
	case Bool:
		return false
	case Float:
		return float64(0.0)
	case Int:
		return 0
	case String:
		return ""
	default:
		return nil
	}
}

func (ft FlagType) String() string {
	switch ft {
	case Bool:
		return "boolean"
	case Float:
		return "float"
	case Int:
		return "integer"
	case String:
		return "string"
	default:
		return "unknown"
	}
}

func getFlagType(value interface{}) FlagType {
	switch value.(type) {
	case bool:
		return Bool
	case float64:
		return Float
	case int:
		return Int
	case string:
		return String
	}
	return unknown

}

type Flag struct {
	// Name of the flag, for a given Name the command-line option
	// becomes --Name.
	Name string
	// Char is an optional single-char alternative
	Char rune
	// The meta variable name that will displayed on help.
	MetaVar string
	// The type of the flag's value.
	Type FlagType
	// Default holds the default value of the flag.
	Default interface{}
	value   interface{}
	// Choices restricts the Values this flag can take to this set.
	Choices interface{}
	// Initialize default value from an environment variable the variable
	// is non-empty.
	EnvVar string
	// Required makes the flag required.
	Required bool
	// Usage is printed to the help screen - short summary of function.
	Usage string
}

func (f *Flag) Set(value string) error {
	var err error
	switch f.Type {
	case Bool:
		lowerCase := strings.ToLower(value)
		if lowerCase == "true" {
			f.value = true
		} else if lowerCase == "false" {
			f.value = false
		} else {
			// actual error handled below
			err = fmt.Errorf("")
		}

	case Float:
		f.value, err = strconv.ParseFloat(value, 64)
	case Int:
		f.value, err = strconv.Atoi(value)
	case String:
		f.value = value
	}
	if err != nil {
		return fmt.Errorf("invalid value for flag %s (type: %s): %s",
			f.Name, f.Type, value)
	}

	return f.Validate()
}

func (f *Flag) String() string {
	usage := f.Usage
	if f.Default != nil {
		usage += fmt.Sprintf(" [%v]", f.Default)
	}
	choices, ok := f.Type.CastSlice(f.Choices)
	if ok && len(choices) > 0 {
		switch f.Type {
		case Int, Float:
			switch len(choices) {
			case 1:
				usage += fmt.Sprintf(" {0-%v}", choices[0])
			case 2:
				usage += fmt.Sprintf(
					" {%v-%v}",
					choices[0],
					choices[1])
			default:
				usage += fmt.Sprintf(
					" {%s}", joinSlice(choices, "|"))
			}
		case String:
			usage += fmt.Sprintf(
				" {%s}", joinSlice(choices, ","))

		}
	}
	return usage
}

func (f *Flag) init() {
	if f.Default != nil {
		f.value = f.Default
	}
	if f.EnvVar != "" {
		envVar := os.Getenv(f.EnvVar)
		if envVar != "" {
			defaultValue := f.value
			err := f.Set(envVar)
			if err != nil {
				// Fall back to default value
				f.value = defaultValue
			}
		}
	}
}

func (f *Flag) Validate() error {
	// Type agnostic validation
	if err := f.validate(); err != nil {
		return err
	}
	// Type specific validation
	return f.validateChoices()
}

// Type agnostic validation
func (f *Flag) validate() error {
	// Check if name is present
	if f.Name == "" {
		return internalError(fmt.Errorf(
			"flag of type %s is missing name",
			f.Type.String()))
	}
	if f.value == nil {
		// Fill in blank value
		f.value = f.Type.Nil()
	}
	// Check that type is correct
	if !f.Type.Equal(f.value) {
		return internalError(fmt.Errorf(
			"flag %s of type %s with illegal value %v (type: %s)",
			f.Name, f.Type, f.value, getFlagType(f.value)))
	}
	// Validate choices' type
	if f.Choices != nil {
		_, ok := f.Type.CastSlice(f.Choices)
		if !ok {
			return internalError(fmt.Errorf(
				"illegal type for choices selection (%v) for "+
					"flag %s with type %s",
				f.Choices, f.Name, f.Type))
		}
	}
	return nil
}

func (f *Flag) validateChoices() error {
	choices, ok := f.Type.CastSlice(f.Choices)
	if !ok {
		return nil
	}
	if len(choices) <= 0 {
		return nil
	}
	switch f.Type {
	case Float:
		switch len(choices) {
		case 1:
			choices = append([]interface{}{0.0}, choices[0])
			fallthrough
		case 2:
			if f.value.(float64) < choices[0].(float64) ||
				f.value.(float64) > choices[1].(float64) {
				return fmt.Errorf(
					"illegal value for flag %s: "+
						"%g not in range [%g, %g]",
					f.Name, f.value.(float64),
					choices[0].(float64),
					choices[1].(float64))
			}
			return nil
		}
	case Int:
		switch len(choices) {
		case 1:
			choices = append([]interface{}{0}, choices[0])
			fallthrough
		case 2:
			if f.value.(int) < choices[0].(int) ||
				f.value.(int) > choices[1].(int) {
				return fmt.Errorf(
					"illegal value for flag %s: "+
						"%d not in range [%d, %d]",
					f.Name, f.value,
					choices[0].(int),
					choices[1].(int))
			}
			return nil
		}
	case Bool:
		return nil
	}
	if !elemInSlice(f.value, choices) {
		return fmt.Errorf(
			"illegal value for flag %s: "+
				"%v not in {%s}", f.Name,
			f.value, joinSlice(choices, ", "))
	}
	return nil
}

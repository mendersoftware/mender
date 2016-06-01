// Copyright 2016 Mender Software AS
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//        http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

package log

import "io"
import "github.com/Sirupsen/logrus"
import logrus_syslog "github.com/Sirupsen/logrus/hooks/syslog"
import "os"
import "runtime"
import "github.com/mendersoftware/scopestack"
import "strings"
import "log/syslog"

type Level logrus.Level

const (
	// PanicLevel level, highest level of severity. Logs and then calls
	// panic with the message passed to Debug, Info, ...
	PanicLevel = logrus.PanicLevel
	// FatalLevel level. Logs and then calls `os.Exit(1)`. It will exit even
	// if the logging level is set to Panic.
	FatalLevel = logrus.FatalLevel
	// ErrorLevel level. Logs. Used for errors that should definitely be
	// noted.
	ErrorLevel = logrus.ErrorLevel
	// WarnLevel level. Non-critical entries that deserve eyes.
	WarnLevel = logrus.WarnLevel
	// InfoLevel level. General operational entries about what's going on
	// inside the application.
	InfoLevel = logrus.InfoLevel
	// DebugLevel level. Usually only enabled when debugging. Very verbose
	// logging.
	DebugLevel = logrus.DebugLevel
)

type Logger struct {
	// Inherit everything from logrus.Logger.
	logrus.Logger

	// Which module is currently active. This will be set in the module
	// field in the log string.
	activeModule string

	// Which modules are not currently active, but on the stack of
	// modules. PushModule() will push the active one onto this stack, put a
	// new one in moduleActive, and PopModule() will pop it and make it
	// active again.
	moduleStack *scopestack.ScopeStack

	// Which filters are active for the modules. Only modules listed here
	// will produce output, unless the list is empty, in which case all
	// modules are printed (subject to log level).
	moduleFilter []string

	// A reference to the hook for the logger.
	loggingHook loggingHookType
}

type loggingHookType struct {
	data *loggingHookData
}

type loggingHookData struct {
	// A hook to the syslog logger, if active.
	// The reason we have to proxy it like this with our own hook, is that
	// logrus doesn't respect that syslog is not connected to a terminal,
	// and will color its output, so we need to turn that off explicitly.
	// See Fire() implementation for the workaround.
	syslogHook *logrus_syslog.SyslogHook
	// A dummy logger for use with the syslogger. All we're interested in
	// here is the formatter.
	syslogLogger *logrus.Logger
}

// A global reference to our logger.
var Log *Logger

func init() {
	Log = New()
}

func (loggingHookType) Levels() []logrus.Level {
	levels := []logrus.Level{logrus.PanicLevel,
		logrus.FatalLevel,
		logrus.ErrorLevel,
		logrus.WarnLevel,
		logrus.InfoLevel}
	return levels
}

func (self loggingHookType) Fire(entry *logrus.Entry) error {
	copy := *entry
	// Make sure we use our own formatter, so we don't get colored output in
	// the syslog.
	copy.Logger = self.data.syslogLogger
	self.data.syslogHook.Fire(&copy)
	return nil
}

// Push a module onto the stack, all future logging calls will be printed with
// this module among the fields, until another module is pushed on top, or this
// one is popped off the stack.
func PushModule(module string) {
	Log.moduleStack.Push(Log.activeModule)
	Log.activeModule = module
}

// Push a module onto the stack, all future logging calls will be printed with
// this module among the fields, until another module is pushed on top, or this
// one is popped off the stack.
func (self *Logger) PushModule(module string) {
	self.moduleStack.Push(self.activeModule)
	self.activeModule = module
}

// Pop a module off the stack, restoring the previous module. This should be
// called from the same function that called PushModule() (for consistency in
// logging.
func PopModule() {
	Log.activeModule = Log.moduleStack.Pop().(string)
}

// Pop a module off the stack, restoring the previous module. This should be
// called from the same function that called PushModule() (for consistency in
// logging.
func (self *Logger) PopModule() {
	self.activeModule = self.moduleStack.Pop().(string)
}

func New() *Logger {
	log := Logger{Logger: *logrus.New()}

	log.Out = os.Stdout
	log.moduleStack = scopestack.NewScopeStack(1)

	return &log
}

func SetModuleFilter(modules []string) {
	Log.SetModuleFilter(modules)
}

func (self *Logger) SetModuleFilter(modules []string) {
	self.moduleFilter = modules
}

// Applies the currently active module in the fields of the log entry.
// Returns nil if log level is not relevant.
func (self *Logger) applyModule(level logrus.Level) *logrus.Entry {
	if level > self.Level {
		return nil
	}

	var module string
	if self.activeModule != "" {
		module = self.activeModule
	} else {

		// Look three functions upwards in the stack, where the log call
		// comes from, and use that as the module name.
		_, file, _, ok := runtime.Caller(3)
		if !ok {
			return self.WithField("module", "<unknown>")
		}
		extPos := strings.LastIndexByte(file, '.')
		if extPos < 0 {
			extPos = len(file)
		}
		lastSlash := strings.LastIndexByte(file, '/')
		if lastSlash < 0 {
			lastSlash = 0
		} else {
			lastSlash += 1
		}
		module = string(file[lastSlash:extPos])
	}

	// Filter based on modules selected.
	if len(self.moduleFilter) > 0 {
		var found bool = false;
		for i := range(self.moduleFilter) {
			if self.moduleFilter[i] == module {
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}

	return self.WithField("module", module)
}

func AddSyslogHook() error {
	return Log.AddSyslogHook()
}

// Add the syslog hook to the logger. This is better than adding it directly,
// for the reasons described in the loggingHookType comments.
func (self *Logger) AddSyslogHook() error {
	hook := loggingHookType{}
	hook.data = &loggingHookData{}
	hook.data.syslogLogger = logrus.New()
	hook.data.syslogLogger.Formatter = &logrus.TextFormatter{
		DisableColors: true,
		DisableTimestamp: true,
	}

	var err error
	hook.data.syslogHook, err = logrus_syslog.NewSyslogHook("", "",
		syslog.LOG_DEBUG, "mender")
	if err != nil {
		return err
	}

	self.loggingHook = hook
	self.Hooks.Add(hook)

	return nil
}


// -----------------------------------------------------------------------------

// Mirror all the logrus API.

func ParseLevel(level string) (logrus.Level, error) {
	return logrus.ParseLevel(level)
}

func AddHook(hook logrus.Hook) {
	Log.Hooks.Add(hook)
}

func Debug(args ...interface{}) {
	Log.debug_impl(args...)
}

func (self *Logger) Debug(args ...interface{}) {
	self.debug_impl(args...)
}

func (self *Logger) debug_impl(args ...interface{}) {
	entry := self.applyModule(logrus.DebugLevel)
	if entry != nil {
		entry.Debug(args...)
	}
}

func Debugf(format string, args ...interface{}) {
	Log.debugf_impl(format, args...)
}

func (self *Logger) Debugf(format string, args ...interface{}) {
	self.debugf_impl(format, args...)
}

func (self *Logger) debugf_impl(format string, args ...interface{}) {
	entry := self.applyModule(logrus.DebugLevel)
	if entry != nil {
		entry.Debugf(format, args...)
	}
}

func Debugln(args ...interface{}) {
	Log.debugln_impl(args...)
}

func (self *Logger) Debugln(args ...interface{}) {
	self.debugln_impl(args...)
}

func (self *Logger) debugln_impl(args ...interface{}) {
	entry := self.applyModule(logrus.DebugLevel)
	if entry != nil {
		entry.Debugln(args...)
	}
}

func Error(args ...interface{}) {
	Log.error_impl(args...)
}

func (self *Logger) Error(args ...interface{}) {
	self.error_impl(args...)
}

func (self *Logger) error_impl(args ...interface{}) {
	entry := self.applyModule(logrus.ErrorLevel)
	if entry != nil {
		entry.Error(args...)
	}
}

func Errorf(format string, args ...interface{}) {
	Log.errorf_impl(format, args...)
}

func (self *Logger) Errorf(format string, args ...interface{}) {
	self.errorf_impl(format, args...)
}

func (self *Logger) errorf_impl(format string, args ...interface{}) {
	entry := self.applyModule(logrus.ErrorLevel)
	if entry != nil {
		entry.Errorf(format, args...)
	}
}

func Errorln(args ...interface{}) {
	Log.errorln_impl(args...)
}

func (self *Logger) Errorln(args ...interface{}) {
	self.errorln_impl(args...)
}

func (self *Logger) errorln_impl(args ...interface{}) {
	entry := self.applyModule(logrus.ErrorLevel)
	if entry != nil {
		entry.Errorln(args...)
	}
}

func Fatal(args ...interface{}) {
	Log.fatal_impl(args...)
}

func (self *Logger) Fatal(args ...interface{}) {
	self.fatal_impl(args...)
}

func (self *Logger) fatal_impl(args ...interface{}) {
	entry := self.applyModule(logrus.FatalLevel)
	if entry != nil {
		entry.Fatal(args...)
	}
}

func Fatalf(format string, args ...interface{}) {
	Log.fatalf_impl(format, args...)
}

func (self *Logger) Fatalf(format string, args ...interface{}) {
	self.fatalf_impl(format, args...)
}

func (self *Logger) fatalf_impl(format string, args ...interface{}) {
	entry := self.applyModule(logrus.FatalLevel)
	if entry != nil {
		entry.Fatalf(format, args...)
	}
}

func Fatalln(args ...interface{}) {
	Log.fatalln_impl(args...)
}

func (self *Logger) Fatalln(args ...interface{}) {
	self.fatalln_impl(args...)
}

func (self *Logger) fatalln_impl(args ...interface{}) {
	entry := self.applyModule(logrus.FatalLevel)
	if entry != nil {
		entry.Fatalln(args...)
	}
}

func Info(args ...interface{}) {
	Log.info_impl(args...)
}

func (self *Logger) Info(args ...interface{}) {
	self.info_impl(args...)
}

func (self *Logger) info_impl(args ...interface{}) {
	entry := self.applyModule(logrus.InfoLevel)
	if entry != nil {
		entry.Info(args...)
	}
}

func Infof(format string, args ...interface{}) {
	Log.infof_impl(format, args...)
}

func (self *Logger) Infof(format string, args ...interface{}) {
	self.infof_impl(format, args...)
}

func (self *Logger) infof_impl(format string, args ...interface{}) {
	entry := self.applyModule(logrus.InfoLevel)
	if entry != nil {
		entry.Infof(format, args...)
	}
}

func Infoln(args ...interface{}) {
	Log.infoln_impl(args...)
}

func (self *Logger) Infoln(args ...interface{}) {
	self.infoln_impl(args...)
}

func (self *Logger) infoln_impl(args ...interface{}) {
	entry := self.applyModule(logrus.InfoLevel)
	if entry != nil {
		entry.Infoln(args...)
	}
}

func IsTerminal() bool {
	return logrus.IsTerminal()
}

func Panic(args ...interface{}) {
	Log.panic_impl(args...)
}

func (self *Logger) Panic(args ...interface{}) {
	self.panic_impl(args...)
}

func (self *Logger) panic_impl(args ...interface{}) {
	entry := self.applyModule(logrus.PanicLevel)
	if entry != nil {
		entry.Panic(args...)
	}
}

func Panicf(format string, args ...interface{}) {
	Log.panicf_impl(format, args...)
}

func (self *Logger) Panicf(format string, args ...interface{}) {
	self.panicf_impl(format, args...)
}

func (self *Logger) panicf_impl(format string, args ...interface{}) {
	entry := self.applyModule(logrus.PanicLevel)
	if entry != nil {
		entry.Panicf(format, args...)
	}
}

func Panicln(args ...interface{}) {
	Log.panicln_impl(args...)
}

func (self *Logger) Panicln(args ...interface{}) {
	self.panicln_impl(args...)
}

func (self *Logger) panicln_impl(args ...interface{}) {
	entry := self.applyModule(logrus.PanicLevel)
	if entry != nil {
		entry.Panicln(args...)
	}
}

func Print(args ...interface{}) {
	Log.print_impl(args...)
}

func (self *Logger) Print(args ...interface{}) {
	self.print_impl(args...)
}

func (self *Logger) print_impl(args ...interface{}) {
	entry := self.applyModule(logrus.PanicLevel)
	if entry != nil {
		entry.Print(args...)
	}
}

func Printf(format string, args ...interface{}) {
	Log.printf_impl(format, args...)
}

func (self *Logger) Printf(format string, args ...interface{}) {
	self.printf_impl(format, args...)
}

func (self *Logger) printf_impl(format string, args ...interface{}) {
	entry := self.applyModule(logrus.PanicLevel)
	if entry != nil {
		entry.Printf(format, args...)
	}
}

func Println(args ...interface{}) {
	Log.println_impl(args...)
}

func (self *Logger) Println(args ...interface{}) {
	self.println_impl(args...)
}

func (self *Logger) println_impl(args ...interface{}) {
	entry := self.applyModule(logrus.PanicLevel)
	if entry != nil {
		entry.Println(args...)
	}
}

func SetFormatter(formatter logrus.Formatter) {
	Log.Formatter = formatter
}

func SetLevel(level logrus.Level) {
	Log.Level = level
}

func SetOutput(out io.Writer) {
	Log.Out = out
}

func Warn(args ...interface{}) {
	Log.warn_impl(args...)
}

func (self *Logger) Warn(args ...interface{}) {
	self.warn_impl(args...)
}

func (self *Logger) warn_impl(args ...interface{}) {
	entry := self.applyModule(logrus.WarnLevel)
	if entry != nil {
		entry.Warn(args...)
	}
}

func Warnf(format string, args ...interface{}) {
	Log.warnf_impl(format, args...)
}

func (self *Logger) Warnf(format string, args ...interface{}) {
	self.warnf_impl(format, args...)
}

func (self *Logger) warnf_impl(format string, args ...interface{}) {
	entry := self.applyModule(logrus.WarnLevel)
	if entry != nil {
		entry.Warnf(format, args...)
	}
}

func Warnln(args ...interface{}) {
	Log.warnln_impl(args...)
}

func (self *Logger) Warnln(args ...interface{}) {
	self.warnln_impl(args...)
}

func (self *Logger) warnln_impl(args ...interface{}) {
	entry := self.applyModule(logrus.WarnLevel)
	if entry != nil {
		entry.Warnln(args...)
	}
}

func Warning(args ...interface{}) {
	Log.warning_impl(args...)
}

func (self *Logger) Warning(args ...interface{}) {
	self.warning_impl(args...)
}

func (self *Logger) warning_impl(args ...interface{}) {
	entry := self.applyModule(logrus.WarnLevel)
	if entry != nil {
		entry.Warning(args...)
	}
}

func Warningf(format string, args ...interface{}) {
	Log.warningf_impl(format, args...)
}

func (self *Logger) Warningf(format string, args ...interface{}) {
	self.warningf_impl(format, args...)
}

func (self *Logger) warningf_impl(format string, args ...interface{}) {
	entry := self.applyModule(logrus.WarnLevel)
	if entry != nil {
		entry.Warningf(format, args...)
	}
}

func Warningln(args ...interface{}) {
	Log.warningln_impl(args...)
}

func (self *Logger) Warningln(args ...interface{}) {
	self.warningln_impl(args...)
}

func (self *Logger) warningln_impl(args ...interface{}) {
	entry := self.applyModule(logrus.WarnLevel)
	if entry != nil {
		entry.Warningln(args...)
	}
}

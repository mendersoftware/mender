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

import (
	"fmt"
	"io"
	"log/syslog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/mendersoftware/scopestack"

	logrus_syslog "github.com/Sirupsen/logrus/hooks/syslog"
)

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

	// we need to use our own hook handling mechanism as original one is broken
	// see: https://github.com/Sirupsen/logrus/issues/401
	logHooks logrus.LevelHooks
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
	levels := []logrus.Level{
		logrus.PanicLevel,
		logrus.FatalLevel,
		logrus.ErrorLevel,
		logrus.WarnLevel,
		logrus.InfoLevel,
	}
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
func (l *Logger) PushModule(module string) {
	l.moduleStack.Push(l.activeModule)
	l.activeModule = module
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
func (l *Logger) PopModule() {
	l.activeModule = l.moduleStack.Pop().(string)
}

func New() *Logger {
	log := Logger{
		Logger:   *logrus.New(),
		logHooks: make(logrus.LevelHooks),
	}

	log.Out = os.Stdout
	log.moduleStack = scopestack.NewScopeStack(1)

	return &log
}

func SetModuleFilter(modules []string) {
	Log.SetModuleFilter(modules)
}

func (l *Logger) SetModuleFilter(modules []string) {
	l.moduleFilter = modules
}

// Applies the currently active module in the fields of the log entry.
// Returns nil if log level is not relevant.
func (l *Logger) applyModule(level logrus.Level) *logrus.Entry {
	var module string
	if l.activeModule != "" {
		module = l.activeModule
	} else {

		// Look three functions upwards in the stack, where the log call
		// comes from, and use that as the module name.
		_, file, _, ok := runtime.Caller(3)
		if !ok {
			return l.WithField("module", "<unknown>")
		}
		extPos := strings.LastIndexByte(file, '.')
		if extPos < 0 {
			extPos = len(file)
		}
		lastSlash := strings.LastIndexByte(file, '/')
		if lastSlash < 0 {
			lastSlash = 0
		} else {
			lastSlash++
		}
		module = string(file[lastSlash:extPos])
	}

	// Filter based on modules selected.
	if len(l.moduleFilter) > 0 {
		found := false
		for i := range l.moduleFilter {
			if l.moduleFilter[i] == module {
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}

	return l.WithField("module", module)
}

func AddSyslogHook() error {
	return Log.AddSyslogHook()
}

// Add the syslog hook to the logger. This is better than adding it directly,
// for the reasons described in the loggingHookType comments.
func (l *Logger) AddSyslogHook() error {
	hook := loggingHookType{}
	hook.data = &loggingHookData{}
	hook.data.syslogLogger = logrus.New()
	hook.data.syslogLogger.Formatter = &logrus.TextFormatter{
		DisableColors:    true,
		DisableTimestamp: true,
	}

	var err error
	hook.data.syslogHook, err = logrus_syslog.NewSyslogHook("", "",
		syslog.LOG_DEBUG, "mender")
	if err != nil {
		return err
	}

	l.loggingHook = hook
	l.logHooks.Add(hook)

	return nil
}

// -----------------------------------------------------------------------------

// Mirror all the logrus API.

func ParseLevel(level string) (logrus.Level, error) {
	return logrus.ParseLevel(level)
}

func AddHook(hook logrus.Hook) {
	Log.logHooks.Add(hook)
}

func IsTerminal() bool {
	return logrus.IsTerminal()
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

func (l *Logger) fireHook(level logrus.Level, entry logrus.Entry, msg string) {
	entry.Time = time.Now()
	entry.Message = msg
	entry.Level = level
	l.logHooks.Fire(level, &entry)
}

func (l *Logger) doLogging(level logrus.Level, args ...interface{}) {
	entry := l.applyModule(level)
	if entry != nil {

		l.fireHook(level, *entry, fmt.Sprint(args...))

		switch level {
		case logrus.DebugLevel:
			entry.Debug(args...)
		case logrus.InfoLevel:
			entry.Info(args...)
		case logrus.WarnLevel:
			entry.Warn(args...)
		case logrus.ErrorLevel:
			entry.Error(args...)
		case logrus.PanicLevel:
			entry.Panic(args...)
		case logrus.FatalLevel:
			entry.Fatal(args...)
		}
	}
}

func (l *Logger) doLoggingln(level logrus.Level, args ...interface{}) {
	entry := l.applyModule(level)
	if entry != nil {

		l.fireHook(level, *entry, fmt.Sprint(args...))

		switch level {
		case logrus.DebugLevel:
			entry.Debugln(args...)
		case logrus.InfoLevel:
			entry.Infoln(args...)
		case logrus.WarnLevel:
			entry.Warnln(args...)
		case logrus.ErrorLevel:
			entry.Errorln(args...)
		case logrus.PanicLevel:
			entry.Panicln(args...)
		case logrus.FatalLevel:
			entry.Fatalln(args...)
		}
	}
}

func (l *Logger) doFLogging(level logrus.Level, format string, args ...interface{}) {
	entry := l.applyModule(level)
	if entry != nil {

		l.fireHook(level, *entry, fmt.Sprintf(format, args...))

		switch level {
		case logrus.DebugLevel:
			entry.Debugf(format, args...)
		case logrus.InfoLevel:
			entry.Infof(format, args...)
		case logrus.WarnLevel:
			entry.Warnf(format, args...)
		case logrus.ErrorLevel:
			entry.Errorf(format, args...)
		case logrus.PanicLevel:
			entry.Panicf(format, args...)
		case logrus.FatalLevel:
			entry.Fatalf(format, args...)
		}
	}
}

func Debug(args ...interface{}) {
	Log.doLogging(logrus.DebugLevel, args...)
}

func Debugf(format string, args ...interface{}) {
	Log.doFLogging(logrus.DebugLevel, format, args...)
}

func Debugln(args ...interface{}) {
	Log.doLoggingln(logrus.DebugLevel, args...)
}

func Info(args ...interface{}) {
	Log.doLogging(logrus.InfoLevel, args...)
}

func Infof(format string, args ...interface{}) {
	Log.doFLogging(logrus.InfoLevel, format, args...)
}

func Infoln(args ...interface{}) {
	Log.doLoggingln(logrus.InfoLevel, args...)
}

func Warn(args ...interface{}) {
	Log.doLogging(logrus.WarnLevel, args...)
}

func Warnf(format string, args ...interface{}) {
	Log.doFLogging(logrus.WarnLevel, format, args...)
}

func Warnln(args ...interface{}) {
	Log.doLoggingln(logrus.WarnLevel, args...)
}

func Warning(args ...interface{}) {
	Log.doLogging(logrus.WarnLevel, args...)
}

func Warningf(format string, args ...interface{}) {
	Log.doFLogging(logrus.WarnLevel, format, args...)
}

func Warningln(args ...interface{}) {
	Log.doLoggingln(logrus.WarnLevel, args...)
}

func Error(args ...interface{}) {
	Log.doLogging(logrus.ErrorLevel, args...)
}

func Errorf(format string, args ...interface{}) {
	Log.doFLogging(logrus.ErrorLevel, format, args...)
}

func Errorln(args ...interface{}) {
	Log.doLoggingln(logrus.ErrorLevel, args...)
}

func Panic(args ...interface{}) {
	Log.doLogging(logrus.PanicLevel, args...)
}

func Panicf(format string, args ...interface{}) {
	Log.doFLogging(logrus.PanicLevel, format, args...)
}

func Panicln(args ...interface{}) {
	Log.doLoggingln(logrus.PanicLevel, args...)
}

func Fatal(args ...interface{}) {
	Log.doLogging(logrus.FatalLevel, args...)
}

func Fatalf(format string, args ...interface{}) {
	Log.doFLogging(logrus.FatalLevel, format, args...)
}

func Fatalln(args ...interface{}) {
	Log.doLoggingln(logrus.FatalLevel, args...)
}

func Print(args ...interface{}) {
	entry := Log.applyModule(logrus.PanicLevel)
	if entry != nil {
		entry.Print(args...)
	}
}

func Printf(format string, args ...interface{}) {
	entry := Log.applyModule(logrus.PanicLevel)
	if entry != nil {
		entry.Printf(format, args...)
	}
}

func Println(args ...interface{}) {
	entry := Log.applyModule(logrus.PanicLevel)
	if entry != nil {
		entry.Println(args...)
	}
}

func (l *Logger) Debug(args ...interface{}) {
	l.doLogging(logrus.DebugLevel, args...)
}

func (l *Logger) Debugf(format string, args ...interface{}) {
	l.doFLogging(logrus.DebugLevel, format, args...)
}

func (l *Logger) Debugln(args ...interface{}) {
	l.doLoggingln(logrus.DebugLevel, args...)
}

func (l *Logger) Info(args ...interface{}) {
	l.doLogging(logrus.InfoLevel, args...)
}

func (l *Logger) Infof(format string, args ...interface{}) {
	l.doFLogging(logrus.InfoLevel, format, args...)
}

func (l *Logger) Infoln(args ...interface{}) {
	l.doLoggingln(logrus.InfoLevel, args...)
}

func (l *Logger) Warn(args ...interface{}) {
	l.doLogging(logrus.WarnLevel, args...)
}

func (l *Logger) Warnf(format string, args ...interface{}) {
	l.doFLogging(logrus.WarnLevel, format, args...)
}

func (l *Logger) Warnln(args ...interface{}) {
	l.doLoggingln(logrus.WarnLevel, args...)
}

func (l *Logger) Warning(args ...interface{}) {
	l.doLogging(logrus.WarnLevel, args...)
}

func (l *Logger) Warningf(format string, args ...interface{}) {
	l.doFLogging(logrus.WarnLevel, format, args...)
}

func (l *Logger) Warningln(args ...interface{}) {
	l.doLoggingln(logrus.WarnLevel, args...)
}

func (l *Logger) Error(args ...interface{}) {
	l.doLogging(logrus.ErrorLevel, args...)
}

func (l *Logger) Errorf(format string, args ...interface{}) {
	l.doFLogging(logrus.ErrorLevel, format, args...)
}

func (l *Logger) Errorln(args ...interface{}) {
	l.doLoggingln(logrus.ErrorLevel, args...)
}

func (l *Logger) Panic(args ...interface{}) {
	l.doLogging(logrus.PanicLevel, args...)
}

func (l *Logger) Panicf(format string, args ...interface{}) {
	l.doFLogging(logrus.PanicLevel, format, args...)
}

func (l *Logger) Panicln(args ...interface{}) {
	l.doLoggingln(logrus.PanicLevel, args...)
}

func (l *Logger) Fatal(args ...interface{}) {
	l.doLogging(logrus.FatalLevel, args...)
}

func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.doFLogging(logrus.FatalLevel, format, args...)
}

func (l *Logger) Fatalln(args ...interface{}) {
	l.doLoggingln(logrus.FatalLevel, args...)
}

func (l *Logger) Print(args ...interface{}) {
	entry := l.applyModule(logrus.PanicLevel)
	if entry != nil {
		entry.Print(args...)
	}
}

func (l *Logger) Printf(format string, args ...interface{}) {
	entry := l.applyModule(logrus.PanicLevel)
	if entry != nil {
		entry.Printf(format, args...)
	}
}

func (l *Logger) Println(args ...interface{}) {
	entry := l.applyModule(logrus.PanicLevel)
	if entry != nil {
		entry.Println(args...)
	}
}

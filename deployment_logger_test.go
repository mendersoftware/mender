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
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mendersoftware/log"
)

func openLogFileWithContent(file, data string) error {
	logF, _ := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	defer logF.Close()
	if _, err := logF.WriteString(data + "\n"); err != nil {
		return errors.New("")
	}
	return nil
}

func logFileContains(file, expected string) bool {
	content := make([]byte, 1024)
	idF, err := os.Open(file)
	if err != nil {
		return false
	}
	defer idF.Close()

	if _, err = idF.Read(content); err != nil {
		return false
	}

	return strings.HasPrefix(string(content), expected)
}

func TestFileLogger(t *testing.T) {
	logger := NewFileLogger("logfile.log")
	defer os.Remove("logfile.log")

	if logger.logFileName != "logfile.log" {
		t.FailNow()
	}
	if _, err := os.Stat("logfile.log"); os.IsNotExist(err) {
		t.FailNow()
	}

	if _, err := logger.Write([]byte("some log")); err != nil {
		t.FailNow()
	}

	if !logFileContains("logfile.log", "some log") {
		t.FailNow()
	}

	if err := logger.Deinit(); err != nil {
		t.FailNow()
	}

	// now, once logger is deinitialized, writing logs should return error
	if _, err := logger.Write([]byte("some other log")); err == nil {
		t.FailNow()
	}
}

func TestLogManagerInit(t *testing.T) {
	logManager := NewDeploymentLogManager("")

	// make sure that logging is disabled by default
	if logManager.loggingEnabled == true {
		t.FailNow()
	}

	// check that logger is not instantiated while creating log manager
	if logManager.logger != nil {
		t.FailNow()
	}

	if err := logManager.WriteLog([]byte("some log")); err != ErrLoggerNotInitialized {
		t.FailNow()
	}
}

func TestLogManagerCheckEnableDisable(t *testing.T) {
	logManager := NewDeploymentLogManager(".")

	if err := logManager.Enable("1234-5678"); err != nil {
		t.FailNow()
	}

	if !logManager.loggingEnabled {
		t.FailNow()
	}

	logFileCreated := fmt.Sprintf(logFileNameScheme, 1, "1234-5678")
	if _, err := os.Stat(logFileCreated); os.IsNotExist(err) {
		t.FailNow()
	}
	defer os.Remove(logFileCreated)

	if logManager.logger == nil {
		t.FailNow()
	}

	if err := logManager.Disable(); err != nil || logManager.loggingEnabled {
		t.FailNow()
	}

	// disabling disabled logging
	if err := logManager.Disable(); err != nil {
		t.FailNow()
	}
}

func TestLogManagerCheckLogging(t *testing.T) {
	logManager := NewDeploymentLogManager(".")

	if err := logManager.WriteLog([]byte("log")); err != ErrLoggerNotInitialized {
		t.FailNow()
	}

	if err := logManager.Enable("1111-2222"); err != nil {
		t.FailNow()
	}
	logFileCreated := fmt.Sprintf(logFileNameScheme, 1, "1111-2222")
	defer os.Remove(logFileCreated)

	var toWriteLog = `{"msg":"some log"}`
	if err := logManager.WriteLog([]byte(toWriteLog)); err != nil {
		t.FailNow()
	}
	if !logFileContains(logFileCreated, toWriteLog) {
		t.FailNow()
	}
}

func createFilesToRotate(num int) []string {
	fileNames := make([]string, num)
	for i := 1; i <= num; i++ {
		name := fmt.Sprintf(logFileNameScheme, i, "1111-2222")
		fileNames = append(fileNames, name)
		os.Create(name)
	}
	return fileNames
}

func removeLogFiles(names []string) {
	for _, name := range names {
		os.Remove(name)
	}
}

func TestLogManagerLogRotation(t *testing.T) {
	// create files with indexes from .0001 to .0010
	files := createFilesToRotate(10)
	defer removeLogFiles(files)

	logManager := NewDeploymentLogManager(".")
	logManager.deploymentID = "1111-2222"

	logFiles, err := logManager.getSortedLogFiles()
	if len(logFiles) != 10 || err != nil {
		t.Fatalf("have files: [%v]\n", logFiles)
	}

	// add some content to the first file
	logFileWithContent := fmt.Sprintf(logFileNameScheme, 1, "1111-2222")
	logContent := `{"msg":"test"}`
	if err = openLogFileWithContent(logFileWithContent, logContent); err != nil {
		t.FailNow()
	}

	// do log rotation
	logManager.Rotate()

	logFiles, err = logManager.getSortedLogFiles()
	if len(logFiles) != logManager.maxLogFiles || err != nil {
		t.FailNow()
	}

	// should not be rotated as deployment ID is the same as the first file
	logFileWithContent = fmt.Sprintf(logFileNameScheme, 1, "1111-2222")
	if !logFileContains(logFileWithContent, logContent) {
		t.FailNow()
	}

	if logFiles[0] != fmt.Sprintf(logFileNameScheme, 5, "1111-2222") {
		t.FailNow()
	}
}

func TestDeploymentLoggingHook(t *testing.T) {
	deploymentLogger := NewDeploymentLogManager("")
	log.AddHook(NewDeploymentLogHook(deploymentLogger))

	log.Info("test1")

	deploymentLogger.Enable("1111-2222")
	logFile := fmt.Sprintf(logFileNameScheme, 1, "1111-2222")
	defer os.Remove(logFile)

	log.Info("test2")
	deploymentLogger.Disable()

	log.Info("test3")

	if !logFileContains(logFile, `{"level":"info","msg":"test2","time":"`) {
		t.FailNow()
	}
}

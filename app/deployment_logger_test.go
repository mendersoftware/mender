// Copyright 2020 Northern.tech AS
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
package app

import (
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/mendersoftware/log"
	"github.com/stretchr/testify/assert"
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

	return strings.Contains(string(content), expected)
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
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)

	logManager := NewDeploymentLogManager(tempDir)

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
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)

	logManager := NewDeploymentLogManager(tempDir)

	if err := logManager.Enable("1234-5678"); err != nil {
		t.FailNow()
	}

	if !logManager.loggingEnabled {
		t.FailNow()
	}

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
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)

	logManager := NewDeploymentLogManager(tempDir)

	if err := logManager.WriteLog([]byte("log")); err != ErrLoggerNotInitialized {
		t.FailNow()
	}

	if err := logManager.Enable("1111-2222"); err != nil {
		t.FailNow()
	}
	logFileName := fmt.Sprintf(logFileNameScheme, 1, "1111-2222")
	logFile := path.Join(tempDir, logFileName)

	var toWriteLog = `{"msg":"some log"}`
	if err := logManager.WriteLog([]byte(toWriteLog)); err != nil {
		t.FailNow()
	}
	if !logFileContains(logFile, toWriteLog) {
		t.FailNow()
	}
}

func createFilesToRotate(location string, num int) []string {
	fileNames := make([]string, num)
	for i := 1; i <= num; i++ {
		name := path.Join(location, fmt.Sprintf(logFileNameScheme, i, "1111-2222"))
		fileNames = append(fileNames, name)
		os.Create(name)
	}
	return fileNames
}

func TestLogManagerLogRotation(t *testing.T) {
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)

	filesToCreate := 10

	// create files with indexes from .0001 to .0010
	createFilesToRotate(tempDir, filesToCreate)

	// add some content to the first file
	logFileWithContent := path.Join(tempDir, fmt.Sprintf(logFileNameScheme, 1, "1111-2222"))
	logContent := `{"msg":"test"}`
	if err := openLogFileWithContent(logFileWithContent, logContent); err != nil {
		t.Fatal("Failed to openLogFileWithContent")
		t.FailNow()
	}

	logManager := NewDeploymentLogManager(tempDir)
	logManager.deploymentID = "1111-2222"

	logFiles, err := logManager.getSortedLogFiles()
	if len(logFiles) != filesToCreate || err != nil {
		t.Fatalf("expected to have %v files; have files: [%v]\n", filesToCreate, logFiles)
	}

	// do log rotation
	logManager.Rotate()

	logFiles, err = logManager.getSortedLogFiles()
	if len(logFiles) != logManager.maxLogFiles || err != nil {
		t.Fatalf("too many files left after rotating; expecting: %v (actual: %v)",
			logManager.maxLogFiles, len(logFiles))
	}

	// test logging with the same deployment id; should not rotate files
	logManager.Enable("1111-2222")
	logFiles, err = logManager.getSortedLogFiles()
	if len(logFiles) != logManager.maxLogFiles || err != nil {
		t.Fatalf("should not rotate files as the deployment id did not change")
	}
	if path.Base(logFiles[len(logFiles)-1]) != fmt.Sprintf(logFileNameScheme, 1, "1111-2222") {
		t.Fatalf("invalid name for the last log file; expecting %v (actual: %v)",
			fmt.Sprintf(logFileNameScheme, 1, "1111-2222"),
			path.Base(logFiles[len(logFiles)-1]))
	}

	// should not be rotated as deployment ID is the same as the first file
	if !logFileContains(logFileWithContent, logContent) {
		t.Fatalf("Logfile does not contain %s\n", logContent)
		t.FailNow()
	}

	logManager.Disable()

	// continue logging with different deployment id; should rotate log files
	logManager.Enable("2222-3333")
	logFiles, err = logManager.getSortedLogFiles()
	if len(logFiles) != logManager.maxLogFiles || err != nil {
		t.Fatalf("should rotate files as the deployment id changed: %v", logFiles)
	}
	if path.Base(logFiles[len(logFiles)-1]) != fmt.Sprintf(logFileNameScheme, 1, "2222-3333") {
		t.Fatalf("expecting: %v; actual: %v [%v]",
			fmt.Sprintf(logFileNameScheme, 1, "2222-3333"), logFiles[len(logFiles)-1], logFiles)
	}
	// should not be rotated as deployment ID is the same as the first file
	logFileWithContent = path.Join(tempDir, fmt.Sprintf(logFileNameScheme, 2, "1111-2222"))
	if !logFileContains(logFileWithContent, logContent) {
		t.Fatalf("2Logfile does not contain %s\n", logContent)
		t.FailNow()
	}
	logManager.Disable()
}

func TestEnabligLogsNoSpceForStoringLogs(t *testing.T) {
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)

	logManager := NewDeploymentLogManager(tempDir)
	// hope we don't have that much space...
	logManager.minLogSizeBytes = math.MaxUint64

	if err := logManager.Enable("1111-2222"); err != ErrNotEnoughSpaceForLogs {
		t.FailNow()
	}

}

func TestDeploymentLoggingHook(t *testing.T) {
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)
	log.SetLevel(log.DebugLevel)

	deploymentLogger := NewDeploymentLogManager(tempDir)
	log.AddHook(NewDeploymentLogHook(deploymentLogger))

	log.Info("test1")

	deploymentLogger.Enable("1111-2222")
	logFile := fmt.Sprintf(logFileNameScheme, 1, "1111-2222")
	fileLocation := path.Join(tempDir, logFile)

	log.Debug("test2")
	deploymentLogger.Disable()

	log.Info("test3")

	contents, _ := ioutil.ReadFile(fileLocation)
	//// test correct format of log messages
	if !logFileContains(fileLocation, `{"level":"debug","message":"test2","timestamp":"`) {
		log.Warn(string(contents))
	}
	log.Info(string(contents))
}

func TestGetLogs(t *testing.T) {
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)

	deploymentLogger := NewDeploymentLogManager(tempDir)

	// test message formatting when have no log file
	logs, err := deploymentLogger.GetLogs("non-existing-log-file")
	assert.NoError(t, err)
	assert.JSONEq(t, `{"messages":[]}`, string(logs))

	// test message formating with correct log file
	// add some content to the first file
	logFileWithContent := path.Join(tempDir, fmt.Sprintf(logFileNameScheme, 1, "1111-2222"))
	logContent := `{"msg":"test"}`
	err = openLogFileWithContent(logFileWithContent, logContent)
	assert.NoError(t, err)

	// check if file really exists
	_, err = deploymentLogger.findLogsForSpecificID("1111-2222")
	assert.NoError(t, err)
	logs, err = deploymentLogger.GetLogs("1111-2222")
	assert.NoError(t, err)
	assert.JSONEq(t, `{"messages":[{"msg":"test"}]}`, string(logs))

	// test message formating with empty log file;
	// below should create empty log file
	deploymentLogger.Enable("1111-3333")

	_, err = deploymentLogger.findLogsForSpecificID("1111-3333")
	assert.NoError(t, err)
	logs, err = deploymentLogger.GetLogs("1111-3333")
	assert.JSONEq(t, `{"messages":[]}`, string(logs))
	assert.NoError(t, err)

	// test broken log entry
	logFileWithContent = path.Join(tempDir, fmt.Sprintf(logFileNameScheme, 1, "1111-4444"))
	logContent = `{"msg":"test"}
{"msg": "broken
{"msg": "test2"}`
	err = openLogFileWithContent(logFileWithContent, logContent)
	assert.NoError(t, err)

	logs, err = deploymentLogger.GetLogs("1111-4444")
	assert.NoError(t, err)
	assert.JSONEq(t, `{"messages":[{"msg":"test"}, {"msg": "test2"}]}`, string(logs))
}

func TestFindLogFiles(t *testing.T) {
	tempDir, _ := ioutil.TempDir("", "logs")
	defer os.RemoveAll(tempDir)

	deploymentLogger := NewDeploymentLogManager(tempDir)

	logs, err := deploymentLogger.findLogsForSpecificID("non-existing-log-file")
	assert.Error(t, err)
	assert.True(t, os.IsNotExist(err))
	assert.Empty(t, logs)

}

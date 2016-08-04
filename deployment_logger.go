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
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// error messages
var (
	ErrLoggerNotInitialized = errors.New("logger not initialized")
)

type FileLogger struct {
	logFileName string
	logFile     io.WriteCloser
}

// NewFileLogger creates instance of file logger; it is initialized
// just before logging is started
func NewFileLogger(name string) *FileLogger {
	// open log file
	logFile, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		// if we can not open file for logging; return nil
		return nil
	}

	// return FileLogger only when logging is possible (we can open log file)
	return &FileLogger{
		logFileName: name,
		logFile:     logFile,
	}
}

func (fl *FileLogger) Write(log []byte) (int, error) {
	return fl.logFile.Write(log)
}

func (fl *FileLogger) Deinit() error {
	return fl.logFile.Close()
}

type DeploymentLogManager struct {
	logLocation  string
	deploymentID string
	logger       *FileLogger
	// how many log files we are keeping in log directory before rotating
	maxLogFiles int
	// it is easy to add logging hook, but not so much remove it;
	// we need a mechanism for emabling and disabling logging
	loggingEnabled bool
}

const baseLogFileName = "deployments"
const logFileNameScheme = baseLogFileName + ".%04d.%s.log"

func NewDeploymentLogManager(logDirLocation string) *DeploymentLogManager {
	return &DeploymentLogManager{
		logLocation: logDirLocation,
		// file logger needs to be instanciated just before writing logs
		//logger:
		// for now we can hardcode this
		maxLogFiles:    5,
		loggingEnabled: false,
	}
}

func (dlm DeploymentLogManager) WriteLog(log []byte) error {
	if dlm.logger == nil {
		return ErrLoggerNotInitialized
	}
	_, err := dlm.logger.Write(log)
	return err
}

func (dlm *DeploymentLogManager) Enable(deploymentID string) error {
	if dlm.loggingEnabled {
		return nil
	}

	dlm.deploymentID = deploymentID

	// we might have new deployment so might need to rotate files
	dlm.Rotate()

	// instanciate logger
	logFileName := fmt.Sprintf(logFileNameScheme, 1, deploymentID)
	dlm.logger = NewFileLogger(filepath.Join(dlm.logLocation, logFileName))
	if dlm.logger == nil {
		return ErrLoggerNotInitialized
	}

	dlm.loggingEnabled = true
	return nil
}

func (dlm *DeploymentLogManager) Disable() error {
	if !dlm.loggingEnabled {
		return nil
	}

	if err := dlm.logger.Deinit(); err != nil {
		return err
	}

	dlm.loggingEnabled = false
	return nil
}

func (dlm DeploymentLogManager) getSortedLogFiles() ([]string, error) {

	// list all the log files in log directory
	logFiles, err :=
		filepath.Glob(filepath.Join(dlm.logLocation, baseLogFileName+".*"))
	if err != nil {
		return nil, err
	}

	sort.Sort(sort.Reverse(sort.StringSlice(logFiles)))
	return logFiles, nil
}

//log naming convention: <base_name>.%04d.<deployment_id>.log
func (dlm DeploymentLogManager) rotateLogFileName(name string) string {
	logFileName := filepath.Base(name)
	nameChunks := strings.Split(logFileName, ".")

	if len(nameChunks) != 4 {
		// we have malformed file name or file is not a log file
		return name
	}
	seq, err := strconv.Atoi(nameChunks[1])
	if err == nil {
		// IDEA: this will allow handling 9999 log files correctly
		// for more we need to change implementation of getSortedLogFiles()
		return fmt.Sprintf(logFileNameScheme, (seq + 1), nameChunks[2])
	}
	return name
}

func (dlm DeploymentLogManager) Rotate() {
	logFiles, err := dlm.getSortedLogFiles()
	if err != nil {
		// can not rotate
		return
	}

	// do we have some log files already
	if len(logFiles) == 0 {
		return
	}

	// check if we need to delete oldest file
	for len(logFiles) > dlm.maxLogFiles {
		os.Remove(logFiles[0])
		logFiles = append(logFiles[1:])
	}

	// check if last file is the one with the current deployment ID
	if strings.Contains(logFiles[0], dlm.deploymentID) {
		fmt.Printf("do not rotate: [%v]", dlm.deploymentID)
		return
	}

	// rename log files; only those not removed
	for i := range logFiles {
		os.Rename(logFiles[i], dlm.rotateLogFileName(logFiles[i]))
	}
}

func (dlm DeploymentLogManager) findLogsForSpecificID(deploymentID string) (string, error) {
	logFiles, err := dlm.getSortedLogFiles()
	if err != nil {
		return "", err
	}

	// look for the file containing given deployment id
	for _, file := range logFiles {
		if strings.Contains(file, deploymentID) {
			return file, nil
		}
	}
	return "", nil
}

// GetLogs is returnig logs as a JSON string. Function is having the same
// signature as json.Marshal() ([]byte, error)
func (dlm DeploymentLogManager) GetLogs(deploymentID string) ([]byte, error) {
	logFileName, err := dlm.findLogsForSpecificID(deploymentID)
	if err != nil {
		return nil, err
	}

	logF, err := os.Open(logFileName)
	if err != nil {
		return nil, err
	}

	defer logF.Close()

	// read log file line by line
	scanner := bufio.NewScanner(logF)

	var logsList []json.RawMessage

	// read log file line by line
	for scanner.Scan() {
		var logLine json.RawMessage
		// check if the log is valid JSON
		err = json.Unmarshal([]byte(scanner.Text()), &logLine)
		if err != nil {
			// we have broken JSON log; just skip it for now
			continue
		}
		// here we should have a list of verified JSON logs
		logsList = append(logsList, logLine)
	}

	if err = scanner.Err(); err != nil {
		return nil, err
	}

	// opaque individual raw JSON entries into `{"messages:" [...]}` format
	type formattedLog struct {
		Messages []json.RawMessage `json:"messages"`
	}
	logs := formattedLog{logsList}

	return json.Marshal(logs)
}

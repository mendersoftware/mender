// Copyright 2023 Northern.tech AS
//
//	Licensed under the Apache License, Version 2.0 (the "License");
//	you may not use this file except in compliance with the License.
//	You may obtain a copy of the License at
//
//	    http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.
package app

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
	"syscall"

	log "github.com/sirupsen/logrus"

	"github.com/mendersoftware/mender/conf"
)

// error messages
var (
	ErrLoggerNotInitialized  = errors.New("logger not initialized")
	ErrNotEnoughSpaceForLogs = errors.New("not enough space for storing logs")
)

type FileLogger struct {
	logFileName string
	logFile     io.WriteCloser
}

// Global deploymentlogger
var DeploymentLogger *DeploymentLogManager

// NewFileLogger creates instance of file logger; it is initialized
// just before logging is started
func NewFileLogger(name string) *FileLogger {
	// open log file
	logFile, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_APPEND|os.O_SYNC, 0600)
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

	minLogSizeBytes uint64
	// it is easy to add logging hook, but not so much remove it;
	// we need a mechanism for emabling and disabling logging
	loggingEnabled bool
}

const baseLogFileName = "deployments"
const logFileNameScheme = baseLogFileName + ".%04d.%s.log"

func NewDeploymentLogManager(logDirLocation string) *DeploymentLogManager {
	return &DeploymentLogManager{
		logLocation: logDirLocation,
		// file logger needs to be instantiated just before writing logs
		//logger:
		// for now we can hardcode this
		maxLogFiles:     5,
		minLogSizeBytes: 1024 * 100, //100kb
		loggingEnabled:  false,
	}
}

func (dlm DeploymentLogManager) WriteLog(log []byte) error {
	if dlm.logger == nil {
		return ErrLoggerNotInitialized
	}
	_, err := dlm.logger.Write(log)
	return err
}

// check if there is enough space to store the logs
func (dlm *DeploymentLogManager) haveEnoughSpaceForStoringLogs() bool {
	var stat syscall.Statfs_t
	_ = syscall.Statfs(dlm.logLocation, &stat)

	// Available blocks * size per block = available space in bytes
	availableSpace := stat.Bavail * uint64(stat.Bsize)
	return availableSpace > dlm.minLogSizeBytes
}

func (dlm *DeploymentLogManager) Enable(deploymentID string) error {
	if dlm.loggingEnabled {
		return nil
	}

	if !dlm.haveEnoughSpaceForStoringLogs() {
		return ErrNotEnoughSpaceForLogs
	}

	dlm.deploymentID = deploymentID

	// we might have new deployment so might need to rotate files
	dlm.Rotate()

	// instantiate logger
	logFileName := fmt.Sprintf(logFileNameScheme, 1, deploymentID)
	dlm.logger = NewFileLogger(filepath.Join(dlm.logLocation, logFileName))

	if dlm.logger == nil {
		return ErrLoggerNotInitialized
	}

	dlm.loggingEnabled = true

	// Useful for updates where client is upgraded.
	log.Infof("Running Mender client version: %s", conf.VersionString())

	return nil
}

func (dlm *DeploymentLogManager) Disable() error {
	if dlm == nil || !dlm.loggingEnabled {
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

// log naming convention: <base_name>.%04d.<deployment_id>.log
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
		return filepath.Join(filepath.Dir(name),
			fmt.Sprintf(logFileNameScheme, (seq+1), nameChunks[2]))
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

	// check if we need to delete the oldest file(s)
	for len(logFiles) > dlm.maxLogFiles {
		os.Remove(logFiles[0])
		logFiles = logFiles[1:]
	}

	// check if last file is the one with the current deployment ID
	if strings.Contains(logFiles[len(logFiles)-1], dlm.deploymentID) {
		return
	}

	// after rotating we should end up with dlm.maxLogFiles-1 files to
	// have a space for creating new log file
	for len(logFiles) > dlm.maxLogFiles-1 {
		_ = os.Remove(logFiles[0])
		logFiles = logFiles[1:]
	}

	// rename log files; only those not removed
	for i := range logFiles {
		_ = os.Rename(logFiles[i], dlm.rotateLogFileName(logFiles[i]))
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
	return "", os.ErrNotExist
}

// GetLogs is returns logs as a JSON []byte string. Function is having the same
// signature as json.Marshal() ([]byte, error)
func (dlm DeploymentLogManager) GetLogs(deploymentID string) ([]byte, error) {
	// opaque individual raw JSON entries into `{"messages:" [...]}` format
	type formattedDeploymentLogs struct {
		Messages []json.RawMessage `json:"messages"`
	}
	// must be initialized as below
	// if we will use `var logsList []json.RawMessage` instead, while marshalling
	// to JSON we will end up with `{"messages":null}` instead of `{"messages":[]}`
	logsList := make([]json.RawMessage, 0)

	logFileName, err := dlm.findLogsForSpecificID(deploymentID)
	// log file for specific deployment id does not exist
	if err == os.ErrNotExist {
		logs := formattedDeploymentLogs{logsList}
		return json.Marshal(logs)
	}

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

	logs := formattedDeploymentLogs{logsList}

	return json.Marshal(logs)
}

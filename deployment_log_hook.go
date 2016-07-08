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

import "github.com/Sirupsen/logrus"

type DeploymentHook struct {
	logManager *DeploymentLogManager
	// we are keeping it here to have logrus dependency in one place
	formater logrus.Formatter
}

func NewDeploymentLogHook(logManager *DeploymentLogManager) *DeploymentHook {
	return &DeploymentHook{
		logManager: logManager,
		formater:   &logrus.JSONFormatter{},
	}
}

// implementation of logrus Hook interface

func (dh DeploymentHook) Levels() []logrus.Level {
	return []logrus.Level{logrus.PanicLevel,
		logrus.FatalLevel,
		logrus.ErrorLevel,
		logrus.WarnLevel,
		logrus.InfoLevel,
		logrus.DebugLevel}
}

func (dh DeploymentHook) Fire(entry *logrus.Entry) error {
	if !dh.logManager.loggingEnabled {
		return nil
	}

	// customize log message to contain only message, level and time
	dLog := logrus.NewEntry(entry.Logger)
	dLog.Message = entry.Message
	dLog.Level = entry.Level
	dLog.Time = entry.Time

	message, err := dh.formater.Format(dLog)
	if err != nil {
		return err
	}

	err = dh.logManager.WriteLog(message)
	return err
}

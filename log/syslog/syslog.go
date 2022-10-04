// Copyright 2022 Northern.tech AS
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
package syslog

import (
	"fmt"
	"log/syslog"

	"github.com/sirupsen/logrus"
	logrus_syslog "github.com/sirupsen/logrus/hooks/syslog"
)

type SyslogHook struct {
	loglevel logrus.Level
	*logrus_syslog.SyslogHook
}

// NewSyslogHook wraps the SyslogHook in logrus, and adds a logrus.Level
// parameter, to make the syslogger respect the user specified logging level.
func NewSyslogHook(network, raddr string,
	priority syslog.Priority, tag string, loglevel logrus.Level) (*SyslogHook, error) {
	fmt.Println("---------- setting up the syslog hook ----------")
	logrus_sysloghook, err := logrus_syslog.NewSyslogHook(network, raddr, priority, tag)
	if err != nil {
		return nil, err
	}
	return &SyslogHook{
		loglevel:   loglevel,
		SyslogHook: logrus_sysloghook,
	}, nil
}

func (hook *SyslogHook) Fire(entry *logrus.Entry) error {
	fmt.Println("Writing to the syslog...")
	return hook.SyslogHook.Fire(entry)
}

func (hook *SyslogHook) Levels() []logrus.Level {
	// Only log level above and including hook.loglevel
	return logrus.AllLevels[:hook.loglevel+1]
}

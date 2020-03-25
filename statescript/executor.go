// Copyright 2019 Northern.tech AS
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

package statescript

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/client"
	"github.com/pkg/errors"
)

const (
	// exitRetryLater - exit code returned if a script requests a retry
	exitRetryLater = 21

	defaultStateScriptRetryInterval time.Duration = 60 * time.Second

	defaultStateScriptRetryTimeout time.Duration = 30 * time.Minute

	defaultStateScriptTimeout time.Duration = 1 * time.Hour
)

type Executor interface {
	ExecuteAll(state, action string, ignoreError bool, report *client.StatusReportWrapper) error
	CheckRootfsScriptsVersion() error
}

type Launcher struct {
	ArtScriptsPath          string
	RootfsScriptsPath       string
	SupportedScriptVersions []int
	Timeout                 int
	RetryInterval           int
	RetryTimeout            int
}

func (l *Launcher) getRetryInterval() time.Duration {

	if l.RetryInterval != 0 {
		return time.Duration(l.RetryInterval) * time.Second
	}
	log.Warningf("No timeout interval set for the retry-scripts. Falling back to default: %s", defaultStateScriptRetryInterval.String())
	return defaultStateScriptRetryInterval
}

func (l *Launcher) getRetryTimeout() time.Duration {

	if l.RetryTimeout != 0 {
		return time.Duration(l.RetryTimeout) * time.Second
	}
	log.Warningf("No total time set for the retry-scripts' timeslot. Falling back to default: %s", defaultStateScriptRetryTimeout.String())
	return defaultStateScriptRetryTimeout
}

func (l Launcher) getTimeout() time.Duration {

	if l.Timeout != 0 {
		return time.Duration(l.Timeout) * time.Second
	}
	log.Debugf("statescript: timeout for executing scripts is not defined; using default of %s seconds", defaultStateScriptTimeout)
	return defaultStateScriptTimeout
}

//TODO: we can optimize for reading directories once and then creating
// a map with all the scripts that needs to be executed.
func (l Launcher) CheckRootfsScriptsVersion() error {
	// first check if we are having some scripts
	scripts, err := ioutil.ReadDir(l.RootfsScriptsPath)
	if err != nil && os.IsNotExist(err) {
		// no scripts; no error
		return nil
	} else if err != nil {
		return errors.Wrap(err, "statescript: can not read rootfs scripts directory")
	}

	if len(scripts) == 0 {
		return nil
	}

	versionFilePath := filepath.Join(l.RootfsScriptsPath, "version")
	f, err := os.Open(versionFilePath)
	if err != nil && os.IsNotExist(err) {
		errmsg := "statescript: The statescript version file is missing. This file needs to be " +
			"present and contain a single number representing which version of the statescript " +
			"support the client is using in order to successfully run statescripts on the client"
		return errors.New(errmsg)
	} else if err != nil {
		return errors.Wrap(err, "statescript")
	}
	ver, err := readVersion(f)
	if _, ok := err.(readVersionParseError); ok {
		errmsg := "statescript: Failed to parse the version file in the statescript directory (%s). " +
			"The file needs to contain a single integer signifying which version of the statescript " +
			"support which this client is using"
		return fmt.Errorf(errmsg, err)
	}
	if err != nil {
		return errors.Wrap(err, "statescript")
	}

	for _, v := range l.SupportedScriptVersions {
		if v == ver {
			return nil
		}
	}
	return errors.Errorf("statescript: unsupported scripts version: %v", ver)
}

func matchVersion(actual int, supported []int, hasScripts bool) error {
	// if there are no scripts to execute we shold not care about the version
	if hasScripts == false {
		return nil
	}

	for _, v := range supported {
		if v == actual {
			return nil
		}
	}

	errmsg := "statescript: The version read from the version file in the statescript directory " +
		"does not match the versions supported by the client, please make sure the file is present " +
		"and formatted correctly (supported: %v; actual: %v)."
	return errors.Errorf(errmsg, supported, actual)
}

func (l Launcher) get(state, action string) ([]os.FileInfo, string, error) {

	sDir := l.ArtScriptsPath
	if state == "Idle" || state == "Sync" || state == "Download" {
		sDir = l.RootfsScriptsPath
	}

	// ReadDir reads the directory named by dirname and returns
	// a list of directory entries sorted by filename.
	// The list returned should be sorted which guarantees correct
	// order of scripts execution.
	files, err := ioutil.ReadDir(sDir)
	if err != nil && os.IsNotExist(err) {
		// no state scripts directory; just move on
		return nil, "", nil
	} else if err != nil {
		return nil, "", errors.Wrap(err, "statescript: can not read scripts directory")
	}

	scripts := make([]os.FileInfo, 0)
	var version int

	for _, file := range files {
		if file.Name() == "version" {
			f, err := os.Open(filepath.Join(sDir, file.Name()))
			if err != nil {
				return nil, "", errors.Wrapf(err, "statescript")
			}
			version, err = readVersion(f)
			if err != nil {
				return nil, "", errors.Wrapf(err, "statescript")
			}
		}

		if strings.Contains(file.Name(), state+"_") &&
			strings.Contains(file.Name(), action) {

			// all scripts must be formated like `ArtifactInstall_Enter_05(_wifi-driver)`(optional)
			re := regexp.MustCompile(`([A-Za-z]+)_(Enter|Leave|Error)_[0-9][0-9](_\S+)?`)
			if len(file.Name()) == len(re.FindString(file.Name())) {
				scripts = append(scripts, file)
			} else {
				log.Warningf("script format mismatch: '%s' will not be run ", file.Name())
			}
		}
	}

	if err := matchVersion(version, l.SupportedScriptVersions,
		len(scripts) != 0); err != nil {
		return nil, "", err
	}

	return scripts, sDir, nil
}

func execute(name string, timeout time.Duration) error {

	cmd := exec.Command(name)

	var stderr io.ReadCloser
	var err error

	if !strings.HasPrefix(name, "Idle") && !strings.HasPrefix(name, "Sync") {
		stderr, err = cmd.StderrPipe()
		if err != nil {
			log.Errorf("statescript: %v", err)
			return errors.Wrap(err, "statescript: unable to open stderr pipe")
		}
	}

	// As child process gets the same PGID as the parent by default, in order
	// to avoid killing Mender when killing process group we are setting
	// new PGID for the executed script and its children.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return err
	}

	timer := time.AfterFunc(timeout, func() {
		// In addition to kill a single process we are sending SIGKILL to
		// process group making sure we are killing the hanging script and
		// all its children.
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	})
	defer timer.Stop()

	var bts []byte
	if stderr != nil {
		bts, err = ioutil.ReadAll(stderr)
		if err != nil {
			log.Error(err)
		}
	}

	if len(bts) > 0 {
		if len(bts) > 10*1024 {
			log.Infof("Collected output (stderr) while running script %s (Truncated to 10KB)\n%s\n---------- end of script output", name, bts[:10*1024])
		} else {
			log.Infof("Collected output (stderr) while running script %s\n%s\n---------- end of script output", name, string(bts))
		}
	}

	if err := cmd.Wait(); err != nil {
		return err
	}
	return nil
}

func retCode(err error) int {
	defaultFailedCode := -1

	if err != nil {
		// try to get the exit code
		if exitError, ok := err.(*exec.ExitError); ok {
			ws := exitError.Sys().(syscall.WaitStatus)
			return ws.ExitStatus()
		} else {
			return defaultFailedCode
		}
	}
	return 0
}

// Catches a script that requests a retry in a loop. Is limited by the total window given to a script demanding a
// retry.
func executeScript(s os.FileInfo, dir string, l Launcher, timeout time.Duration, ignoreError bool) error {

	iet := time.Now()
	for {
		err := execute(filepath.Join(dir, s.Name()), timeout)
		switch ret := retCode(err); ret {
		case 0:
			// success
			return nil
		case exitRetryLater:
			if time.Since(iet) <= l.getRetryTimeout() {
				log.Infof("statescript: %s requested a retry", s.Name())
				time.Sleep(l.getRetryInterval())
				continue
			}
			if ignoreError {
				log.Errorf("statescript: ignoring error executing '%s': %d: %s", s.Name(), ret, err.Error())
				return nil
			}
			return errors.Errorf("statescript: retry time-limit exceeded %s", err.Error())
		default:
			// In case of error scripts all should be executed.
			if ignoreError {
				log.Errorf("statescript: ignoring error executing '%s': %d: %s", s.Name(), ret, err.Error())
				return nil
			}
			return errors.Errorf("statescript: error executing '%s': %d : %s",
				s.Name(), ret, err.Error())
		}
	}
}

func reportScriptStatus(rep *client.StatusReportWrapper, statusReport string) error {
	c := client.NewStatus()

	return c.Report(rep.API, rep.URL, client.StatusReport{
		DeploymentID: rep.Report.DeploymentID,
		Status:       rep.Report.Status,
		SubState:     statusReport,
	})
}

func (l Launcher) ExecuteAll(state, action string, ignoreError bool,
	report *client.StatusReportWrapper) error {
	scr, dir, err := l.get(state, action)
	if err != nil {
		if ignoreError {
			log.Errorf("statescript: got an error when trying to execute [%s:%s] script, "+
				"but ignoreError is set to true, so continuing. Full error message: %v",
				state, action, err)
			return nil
		}
		return err
	}

	execBits := os.FileMode(syscall.S_IXUSR | syscall.S_IXGRP | syscall.S_IXOTH)
	timeout := l.getTimeout()

	for _, s := range scr {
		// check if script is executable
		if s.Mode()&execBits == 0 {
			if ignoreError {
				log.Errorf("statescript: ignoring script '%s' being not executable",
					filepath.Join(dir, s.Name()))
				continue
			} else {
				return errors.Errorf("statescript: script '%s' is not executable",
					filepath.Join(dir, s.Name()))
			}
		}

		subStatus := fmt.Sprintf("start executing script: %s", s.Name())
		log.Debugf(subStatus)
		if report != nil {
			if err = reportScriptStatus(report, subStatus); err != nil {
				log.Errorf("statescript: can not send start status to server: %s", err.Error())
			}

			defer func() {
				if err = reportScriptStatus(report,
					fmt.Sprintf("finished executing script: %s", s.Name())); err != nil {
					log.Errorf("statescript: can not send finished status to server: %s", err.Error())
				}
			}()
		}

		if err = executeScript(s, dir, l, timeout, ignoreError); err != nil {
			return err
		}
	}
	return nil
}

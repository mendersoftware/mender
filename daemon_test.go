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
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type fakeDevice struct {
	retReboot         error
	retInstallUpdate  error
	retEnablePart     error
	retCommit         error
	retRollback       error
	retHasUpdate      bool
	retHasUpdateError error
	consumeUpdate     bool
}

func (f fakeDevice) Reboot() error {
	return f.retReboot
}

func (f fakeDevice) Rollback() error {
	return f.retRollback
}

func (f fakeDevice) InstallUpdate(from io.ReadCloser, sz int64) error {
	if f.consumeUpdate {
		_, err := io.Copy(ioutil.Discard, from)
		return err
	}
	return f.retInstallUpdate
}

func (f fakeDevice) EnableUpdatedPartition() error {
	return f.retEnablePart
}

func (f fakeDevice) CommitUpdate() error {
	return f.retCommit
}

func (f fakeDevice) HasUpdate() (bool, error) {
	return f.retHasUpdate, f.retHasUpdateError
}

type fakeUpdater struct {
	GetScheduledUpdateReturnIface interface{}
	GetScheduledUpdateReturnError error
	fetchUpdateReturnReadCloser   io.ReadCloser
	fetchUpdateReturnSize         int64
	fetchUpdateReturnError        error
}

func (f fakeUpdater) GetScheduledUpdate(api ApiRequester, url string) (interface{}, error) {
	return f.GetScheduledUpdateReturnIface, f.GetScheduledUpdateReturnError
}
func (f fakeUpdater) FetchUpdate(api ApiRequester, url string) (io.ReadCloser, int64, error) {
	return f.fetchUpdateReturnReadCloser, f.fetchUpdateReturnSize, f.fetchUpdateReturnError
}

func fakeProcessUpdate(response *http.Response) (interface{}, error) {
	return nil, nil
}

type fakePreDoneState struct {
	BaseState
}

func (f *fakePreDoneState) Handle(ctx *StateContext, c Controller) (State, bool) {
	return doneState, false
}

func TestDaemon(t *testing.T) {
	store := NewMemStore()
	mender := newTestMender(nil, menderConfig{},
		testMenderPieces{
			MenderPieces: MenderPieces{
				store: store,
			},
		})
	d := NewDaemon(mender, store)

	mender.SetState(&fakePreDoneState{
		BaseState{
			MenderStateInit,
		},
	})
	err := d.Run()
	assert.NoError(t, err)
}

type daemonTestController struct {
	stateTestController
	updateCheckCount int
}

func (d *daemonTestController) CheckUpdate() (*UpdateResponse, menderError) {
	d.updateCheckCount = d.updateCheckCount + 1
	return d.stateTestController.CheckUpdate()
}

func (d *daemonTestController) RunState(ctx *StateContext) (State, bool) {
	return d.state.Handle(ctx, d)
}

func TestDaemonRun(t *testing.T) {

	if testing.Short() {
		t.Skip("skipping periodic update check in short tests")
	}

	pollInterval := time.Duration(10) * time.Millisecond

	dtc := &daemonTestController{
		stateTestController{
			pollIntvl: pollInterval,
			state:     initState,
		},
		0,
	}
	daemon := NewDaemon(dtc, NewMemStore())

	tempDir, _ := ioutil.TempDir("", "logs")
	DeploymentLogger = NewDeploymentLogManager(tempDir)
	defer os.RemoveAll(tempDir)

	go daemon.Run()

	timespolled := 5
	time.Sleep(time.Duration(timespolled) * pollInterval)
	daemon.StopDaemon()

	t.Logf("poke count: %v", dtc.updateCheckCount)
	assert.False(t, dtc.updateCheckCount < (timespolled-1))
}

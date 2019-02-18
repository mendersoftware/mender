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
package main

import (
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/datastore"
	"github.com/mendersoftware/mender/installer"
	"github.com/mendersoftware/mender/store"
	"github.com/stretchr/testify/assert"
)

type fakeDevice struct {
	retReboot      error
	retStoreUpdate error
	retEnablePart  error
	retCommit      error
	retRollback    error
	retHasUpdate   bool
	consumeUpdate  bool
}

func (f fakeDevice) NeedsReboot() (installer.NeedsRebootType, error) {
	return installer.NeedsRebootYes, nil
}

func (f fakeDevice) SupportsRollback() (bool, error) {
	return true, nil
}

func (f fakeDevice) Reboot() error {
	return f.retReboot
}

func (f fakeDevice) RollbackReboot() error {
	return f.retReboot
}

func (f fakeDevice) Rollback() error {
	return f.retRollback
}

func (f fakeDevice) Initialize(artifactHeaders,
	artifactAugmentedHeaders artifact.HeaderInfoer,
	payloadHeaders handlers.ArtifactUpdateHeaders) error {

	return nil
}

func (f fakeDevice) PrepareStoreUpdate() error {
	return nil
}

func (f fakeDevice) StoreUpdate(from io.Reader, info os.FileInfo) error {
	if f.consumeUpdate {
		_, err := io.Copy(ioutil.Discard, from)
		return err
	}
	return f.retStoreUpdate
}

func (f fakeDevice) FinishStoreUpdate() error {
	return nil
}

func (f fakeDevice) InstallUpdate() error {
	return f.retEnablePart
}

func (f fakeDevice) CommitUpdate() error {
	return f.retCommit
}

func (f fakeDevice) VerifyReboot() error {
	if f.retHasUpdate {
		return nil
	} else {
		return errors.New("No update")
	}
}

func (f fakeDevice) VerifyRollbackReboot() error {
	if f.retHasUpdate {
		return errors.New("Not able to roll back")
	} else {
		return nil
	}
}

func (f fakeDevice) Failure() error {
	return nil
}
func (f fakeDevice) Cleanup() error {
	return nil
}

func (f fakeDevice) GetActive() (string, error) {
	return "", errors.New("Not implemented")
}

func (f fakeDevice) GetInactive() (string, error) {
	return "", errors.New("Not implemented")
}

func (f fakeDevice) NewUpdateStorer(string, int) (handlers.UpdateStorer, error) {
	return &f, nil
}

func (f fakeDevice) GetType() string {
	return "rootfs-image"
}

type fakeUpdater struct {
	GetScheduledUpdateReturnIface interface{}
	GetScheduledUpdateReturnError error
	fetchUpdateReturnReadCloser   io.ReadCloser
	fetchUpdateReturnSize         int64
	fetchUpdateReturnError        error
}

func (f fakeUpdater) GetScheduledUpdate(api client.ApiRequester, url string) (interface{}, error) {
	return f.GetScheduledUpdateReturnIface, f.GetScheduledUpdateReturnError
}
func (f fakeUpdater) FetchUpdate(api client.ApiRequester, url string) (io.ReadCloser, int64, error) {
	return f.fetchUpdateReturnReadCloser, f.fetchUpdateReturnSize, f.fetchUpdateReturnError
}

func fakeProcessUpdate(response *http.Response) (interface{}, error) {
	return nil, nil
}

type fakePreDoneState struct {
	baseState
}

func (f *fakePreDoneState) Handle(ctx *StateContext, c Controller) (State, bool) {
	return doneState, false
}

func TestDaemon(t *testing.T) {
	store := store.NewMemStore()
	mender := newTestMender(nil, menderConfig{},
		testMenderPieces{
			MenderPieces: MenderPieces{
				store: store,
			},
		})
	mender.state = &fakePreDoneState{
		baseState{
			id: datastore.MenderStateInit,
		},
	}

	d := NewDaemon(mender, store)

	err := d.Run()
	assert.NoError(t, err)
}

func TestDaemonCleanup(t *testing.T) {
	mstore := &store.MockStore{}
	mstore.On("Close").Return(nil)
	d := NewDaemon(nil, mstore)
	d.Cleanup()
	mstore.AssertExpectations(t)

	mstore = &store.MockStore{}
	mstore.On("Close").Return(errors.New("foo"))
	assert.NotPanics(t, func() {
		d := NewDaemon(nil, mstore)
		d.Cleanup()
	})
	mstore.AssertExpectations(t)
}

type daemonTestController struct {
	stateTestController
	updateCheckCount int
}

func (d *daemonTestController) CheckUpdate() (*datastore.UpdateInfo, menderError) {
	d.updateCheckCount++
	return d.stateTestController.CheckUpdate()
}

func (d *daemonTestController) TransitionState(next State, ctx *StateContext) (State, bool) {
	next, cancel := d.state.Handle(ctx, d)
	d.state = next
	return next, cancel
}

func TestDaemonRun(t *testing.T) {
	t.Run("Testrun daemon", func(t *testing.T) {
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
		daemon := NewDaemon(dtc, store.NewMemStore())
		dtc.state = initState
		dtc.authorized = true

		tempDir, _ := ioutil.TempDir("", "logs")
		DeploymentLogger = NewDeploymentLogManager(tempDir)
		defer os.RemoveAll(tempDir)

		go daemon.Run()

		timespolled := 5
		time.Sleep(time.Duration(timespolled) * pollInterval)
		daemon.StopDaemon()

		t.Logf("poke count: %v", dtc.updateCheckCount)
		assert.False(t, dtc.updateCheckCount < (timespolled-1))

	})
	t.Run("testing state machine interrupt functionality", func(t *testing.T) {
		pollInterval := time.Duration(10) * time.Millisecond
		dtc := &daemonTestController{
			stateTestController{
				pollIntvl: pollInterval,
				state:     initState,
			},
			0,
		}
		daemon := NewDaemon(dtc, store.NewMemStore())
		dtc.state = checkWaitState
		dtc.pollIntvl = time.Second * 5
		dtc.retryIntvl = time.Second * 5
		dtc.authorized = true
		daemon.StopDaemon()                        // Stop after a single pass.
		go func() { daemon.updateCheck <- true }() // Force updateCheck state.
		time.Sleep(time.Second * 1)                // Make sure the signal has been sent.
		daemon.Run()
		assert.Equal(t, daemon.mender.GetCurrentState(), checkWaitState)
		daemon.StopDaemon()
	})
}

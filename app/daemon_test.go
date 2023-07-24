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
	"errors"
	"io"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/datastore"
	"github.com/mendersoftware/mender/installer"
	"github.com/mendersoftware/mender/store"
)

type FakeDevice struct {
	RetReboot              error
	RetStoreUpdate         error
	RetEnablePart          error
	RetCommit              error
	RetRollback            error
	RetHasUpdate           bool
	ConsumeUpdate          bool
	NeedsRebootReturnValue *installer.RebootAction
}

func (f FakeDevice) NeedsReboot() (installer.RebootAction, error) {
	if f.NeedsRebootReturnValue != nil {
		return *f.NeedsRebootReturnValue, nil
	}
	return installer.RebootRequired, nil
}

func (f FakeDevice) SupportsRollback() (bool, error) {
	return true, nil
}

func (f FakeDevice) Reboot() error {
	return f.RetReboot
}

func (f FakeDevice) RollbackReboot() error {
	return f.RetReboot
}

func (f FakeDevice) Rollback() error {
	return f.RetRollback
}

func (f FakeDevice) Initialize(artifactHeaders,
	artifactAugmentedHeaders artifact.HeaderInfoer,
	payloadHeaders handlers.ArtifactUpdateHeaders) error {

	return nil
}

func (f FakeDevice) PrepareStoreUpdate() error {
	return nil
}

func (f FakeDevice) StoreUpdate(from io.Reader, info os.FileInfo) error {
	if f.ConsumeUpdate {
		_, err := io.Copy(ioutil.Discard, from)
		return err
	}
	return f.RetStoreUpdate
}

func (f FakeDevice) FinishStoreUpdate() error {
	return nil
}

func (f FakeDevice) InstallUpdate() error {
	return f.RetEnablePart
}

func (f FakeDevice) CommitUpdate() error {
	return f.RetCommit
}

func (f FakeDevice) VerifyReboot() error {
	if f.RetHasUpdate {
		return nil
	} else {
		return errors.New("No update")
	}
}

func (f FakeDevice) VerifyRollbackReboot() error {
	if f.RetHasUpdate {
		return errors.New("Not able to roll back")
	} else {
		return nil
	}
}

func (f FakeDevice) Failure() error {
	return nil
}
func (f FakeDevice) Cleanup() error {
	return nil
}

func (f FakeDevice) GetActive() (string, error) {
	return "", errors.New("Not implemented")
}

func (f FakeDevice) GetInactive() (string, error) {
	return "", errors.New("Not implemented")
}

func (f FakeDevice) NewUpdateStorer(*string, int) (handlers.UpdateStorer, error) {
	return &f, nil
}

func (f FakeDevice) GetType() string {
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

func (f fakeUpdater) FetchUpdate(
	api client.ApiRequester,
	url string,
) (io.ReadCloser, int64, error) {
	return f.fetchUpdateReturnReadCloser, f.fetchUpdateReturnSize, f.fetchUpdateReturnError
}

type fakePreDoneState struct {
	baseState
}

func (f *fakePreDoneState) Handle(ctx *StateContext, c Controller) (State, bool) {
	return States.Final, false
}

func TestDaemon(t *testing.T) {
	store := store.NewMemStore()
	mender, authManager := newTestMenderAndAuthManager(conf.MenderConfig{},
		testMenderPieces{
			MenderPieces: MenderPieces{
				Store: store,
			},
		})
	mender.state = &fakePreDoneState{
		baseState{
			id: datastore.MenderStateInit,
		},
	}

	d, err := NewDaemon(&conf.MenderConfig{}, mender, store, authManager)
	require.NoError(t, err)

	err = d.Run()
	assert.NoError(t, err)
}

func TestDaemonCleanup(t *testing.T) {
	mstore := &store.MockStore{}
	mstore.On("ReadAll", "update-control-maps").Return(nil, os.ErrNotExist)
	mstore.On("Close").Return(nil)
	mender, err := NewMender(&conf.MenderConfig{}, MenderPieces{Store: mstore})
	require.NoError(t, err)
	d, err := NewDaemon(&conf.MenderConfig{}, mender, mstore, nil)
	require.NoError(t, err)
	d.Cleanup()
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

func (d *daemonTestController) TransitionState(_ State, ctx *StateContext) (State, bool) {
	next, cancel := d.state.Handle(ctx, d)
	d.state = next
	return next, cancel
}

// dummy - always true
func (d *daemonTestController) RefreshServerUpdateControlMap(deploymentID string) error {
	return nil
}

func TestDaemonRun(t *testing.T) {
	t.Run("Testrun daemon", func(t *testing.T) {
		t.Parallel()

		pollInterval := time.Duration(1) * time.Second

		dtc := &daemonTestController{
			stateTestController{
				updatePollIntvl: pollInterval,
				inventPollIntvl: pollInterval,
				state:           States.Init,
			},
			0,
		}
		daemon, err := NewDaemon(&conf.MenderConfig{}, dtc, store.NewMemStore(), nil)
		require.NoError(t, err)
		dtc.state = States.Init
		dtc.authorized = true

		go daemon.Run()

		timespolled := 5
		time.Sleep(time.Duration(timespolled) * pollInterval)
		daemon.StopDaemon()

		t.Logf("poke count: %v", dtc.updateCheckCount)
		assert.GreaterOrEqual(t, dtc.updateCheckCount, (timespolled - 1))
		assert.LessOrEqual(t, dtc.updateCheckCount, (timespolled + 1))

	})
	t.Run("testing state machine interrupt functionality - updateCheck state", func(t *testing.T) {
		pollInterval := time.Duration(30) * time.Second
		dtc := &daemonTestController{
			stateTestController{
				updatePollIntvl: pollInterval,
				state:           States.Idle,
			},
			0,
		}
		daemon, err := NewDaemon(&conf.MenderConfig{}, dtc, store.NewMemStore(), nil)
		require.NoError(t, err)
		dtc.authorized = true
		daemon.StopDaemon()                                       // Stop after a single pass.
		go func() { daemon.ForceToState <- States.UpdateCheck }() // Force updateCheck state.
		time.Sleep(
			time.Second * 1,
		) // Make sure the signal has been sent.
		daemon.Run()
		assert.Equal(t, States.CheckWait, daemon.Mender.GetCurrentState())
	})
	t.Run(
		"testing state machine interrupt functionality - inventoryUpdate state",
		func(t *testing.T) {
			pollInterval := time.Duration(30) * time.Second
			dtc := &daemonTestController{
				stateTestController{
					inventPollIntvl: pollInterval,
					state:           States.Idle,
				},
				0,
			}
			daemon, err := NewDaemon(&conf.MenderConfig{}, dtc, store.NewMemStore(), nil)
			require.NoError(t, err)
			dtc.authorized = true
			daemon.StopDaemon()                                           // Stop after a single pass.
			go func() { daemon.ForceToState <- States.InventoryUpdate }() // Force inventoryUpdate state.
			time.Sleep(
				time.Second * 1,
			) // Make sure the signal has been sent.
			daemon.Run()
			assert.Equal(t, States.CheckWait, daemon.Mender.GetCurrentState())
		},
	)
}

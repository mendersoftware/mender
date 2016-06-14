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
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeDevice struct {
	retReboot        error
	retInstallUpdate error
	retEnablePart    error
	retCommit        error
}

func (f fakeDevice) Reboot() error {
	return f.retReboot
}

func (f fakeDevice) InstallUpdate(io.ReadCloser, int64) error {
	return f.retInstallUpdate
}

func (f fakeDevice) EnableUpdatedPartition() error {
	return f.retEnablePart
}

func (f fakeDevice) CommitUpdate() error {
	return f.retCommit
}

type fakeUpdater struct {
	GetScheduledUpdateReturnIface interface{}
	GetScheduledUpdateReturnError error
	fetchUpdateReturnReadCloser   io.ReadCloser
	fetchUpdateReturnSize         int64
	fetchUpdateReturnError        error
}

func (f fakeUpdater) GetScheduledUpdate(url string, device string) (interface{}, error) {
	return f.GetScheduledUpdateReturnIface, f.GetScheduledUpdateReturnError
}
func (f fakeUpdater) FetchUpdate(url string) (io.ReadCloser, int64, error) {
	return f.fetchUpdateReturnReadCloser, f.fetchUpdateReturnSize, f.fetchUpdateReturnError
}

func fakeProcessUpdate(response *http.Response) (interface{}, error) {
	return nil, nil
}

type fakePreDoneState struct {
	BaseState
}

func (f *fakePreDoneState) Handle(c Controller) (State, bool) {
	return doneState, false
}

func TestDaemon(t *testing.T) {
	mender := newDefaultTestMender()
	d := NewDaemon(mender)

	mender.SetState(&fakePreDoneState{
		BaseState{
			MenderStateInit,
		},
	})
	err := d.Run()
	assert.NoError(t, err)
}


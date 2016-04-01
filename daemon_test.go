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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func (f fakeUpdater) GetScheduledUpdate(process RequestProcessingFunc,
	url string, device string) (interface{}, error) {
	return f.GetScheduledUpdateReturnIface, f.GetScheduledUpdateReturnError
}
func (f fakeUpdater) FetchUpdate(url string) (io.ReadCloser, int64, error) {
	return f.fetchUpdateReturnReadCloser, f.fetchUpdateReturnSize, f.fetchUpdateReturnError
}

func fakeProcessUpdate(response *http.Response) (interface{}, error) {
	return nil, nil
}

func Test_checkUpdate_errorAskingForUpdate_returnsNoUpdate(t *testing.T) {
	updater := fakeUpdater{}
	updater.GetScheduledUpdateReturnError = errors.New("fake error")

	if _, haveUpdate := checkScheduledUpdate(updater, fakeProcessUpdate, nil, "", ""); haveUpdate {
		t.FailNow()
	}
}

func Test_checkUpdate_askingForUpdateReturnsEmpty_returnsNoUpdate(t *testing.T) {
	updater := fakeUpdater{}
	updater.GetScheduledUpdateReturnIface = ""

	if _, haveUpdate := checkScheduledUpdate(updater, fakeProcessUpdate, nil, "", ""); haveUpdate {
		t.FailNow()
	}
}

func Test_checkUpdate_askingForUpdateReturnsUpdate_returnsHaveUpdate(t *testing.T) {
	updater := fakeUpdater{}
	updater.GetScheduledUpdateReturnIface = UpdateResponse{}
	update := UpdateResponse{}

	if _, haveUpdate := checkScheduledUpdate(updater, fakeProcessUpdate, &update, "", ""); !haveUpdate {
		t.FailNow()
	}
}

func Test_fetchAndInstallUpdate_updateFetchError_returnsNotInstalled(t *testing.T) {
	updater := fakeUpdater{}
	updater.GetScheduledUpdateReturnIface = new(UpdateResponse)
	updater.fetchUpdateReturnError = errors.New("")
	daemon := menderDaemon{}
	daemon.Updater = updater

	if installed := fetchAndInstallUpdate(&daemon, UpdateResponse{}); installed {
		t.FailNow()
	}
}

func Test_fetchAndInstallUpdate_installError_returnsNotInstalled(t *testing.T) {
	updater := fakeUpdater{}
	updater.GetScheduledUpdateReturnIface = new(UpdateResponse)
	device := fakeDevice{}
	device.retInstallUpdate = errors.New("")
	daemon := menderDaemon{}
	daemon.Updater = updater
	daemon.UInstallCommitRebooter = device

	if installed := fetchAndInstallUpdate(&daemon, UpdateResponse{}); installed {
		t.FailNow()
	}
}

func Test_fetchAndInstallUpdate_updatePartitionError_returnsNotInstalled(t *testing.T) {
	updater := fakeUpdater{}
	updater.GetScheduledUpdateReturnIface = new(UpdateResponse)
	device := fakeDevice{}
	device.retEnablePart = errors.New("")
	daemon := menderDaemon{}
	daemon.Updater = updater
	daemon.UInstallCommitRebooter = device

	if installed := fetchAndInstallUpdate(&daemon, UpdateResponse{}); installed {
		t.FailNow()
	}
}

func Test_fetchAndInstallUpdate_noErrors_returnsInstalled(t *testing.T) {
	updater := fakeUpdater{}
	updater.GetScheduledUpdateReturnIface = new(UpdateResponse)
	device := fakeDevice{}
	daemon := menderDaemon{}
	daemon.Updater = updater
	daemon.UInstallCommitRebooter = device

	if installed := fetchAndInstallUpdate(&daemon, UpdateResponse{}); !installed {
		t.FailNow()
	}
}

func Test_checkPeriodicDaemonUpdate_haveServerAndCorrectResponse_FetchesUpdate(t *testing.T) {
	reqHandlingCnt := 0
	pollInterval := time.Duration(10) * time.Millisecond

	// Test server that always responds with 200 code, and specific payload
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, correctUpdateResponse)
		reqHandlingCnt++
	}))
	defer ts.Close()

	client := NewHttpsClient(httpsClientConfig{"client.crt", "client.key", "server.crt", true})
	device := NewDevice(nil, nil, "")
	runner := newTestOSCalls("", 0)
	fakeEnv := uBootEnv{&runner}
	controler := NewMender(&fakeEnv)
	daemon := NewDaemon(client, device, controler)
	daemon.config = daemonConfig{serverpollInterval: pollInterval, serverURL: ts.URL}

	go daemon.Run()

	timespolled := 5
	time.Sleep(time.Duration(timespolled) * pollInterval)
	daemon.StopDaemon()

	if reqHandlingCnt < (timespolled - 1) {
		t.Fatal("Expected to receive at least ", timespolled-1, " requests - ", reqHandlingCnt, " received")
	}
}

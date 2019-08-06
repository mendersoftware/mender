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
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/datastore"
	"github.com/mendersoftware/mender/installer"
	"github.com/mendersoftware/mender/statescript"
	"github.com/mendersoftware/mender/store"
	"github.com/mendersoftware/mender/utils"
	"github.com/pkg/errors"
)

type standaloneData struct {
	artifactName string
	installers   []installer.PayloadUpdatePerformer
}

// This will be run manually from command line ONLY
func doStandaloneInstall(device *deviceManager, args runOptionsType,
	vKey []byte, stateExec statescript.Executor) error {

	var image io.ReadCloser
	var imageSize int64
	var err error
	var upclient client.Updater

	if args == (runOptionsType{}) {
		return errors.New("Image file called without needed parameters")
	}

	log.Debug("Starting device update.")

	updateLocation := *args.imageFile
	if strings.HasPrefix(updateLocation, "http:") ||
		strings.HasPrefix(updateLocation, "https:") {
		log.Infof("Performing remote update from: [%s].", updateLocation)

		var ac *client.ApiClient
		// we are having remote update
		ac, err = client.New(args.Config)
		if err != nil {
			return errors.New("Can not initialize client for performing network update.")
		}
		upclient = client.NewUpdate()

		log.Debug("Client initialized. Start downloading image.")

		image, imageSize, err = upclient.FetchUpdate(ac, updateLocation, 0)
		log.Debugf("Image downloaded: %d [%v] [%v]", imageSize, image, err)
	} else {
		// perform update from local file
		log.Infof("Start updating from local image file: [%s]", updateLocation)
		image, imageSize, err = installer.FetchUpdateFromFile(updateLocation)

		log.Debugf("Fetching update from file results: [%v], %d, %v", image, imageSize, err)
	}

	if image == nil || err != nil {
		return errors.Wrapf(err, "Error while installing Artifact from command line")
	}
	defer image.Close()

	fmt.Fprintf(os.Stdout, "Installing Artifact of size %d...\n", imageSize)
	p := &utils.ProgressWriter{
		Out: os.Stdout,
		N:   imageSize,
	}
	tr := io.TeeReader(image, p)

	return doStandaloneInstallStates(ioutil.NopCloser(tr), vKey, device, stateExec)
}

func doStandaloneInstallStatesDownload(art io.ReadCloser, key []byte,
	device *deviceManager, stateExec statescript.Executor) (*standaloneData, error) {

	dt, err := device.GetDeviceType()
	if err != nil {
		log.Errorf("Could not determine device type: %s", err.Error())
		return nil, err
	}

	// Download state
	err = stateExec.ExecuteAll("Download", "Enter", false, nil)
	if err != nil {
		log.Errorf("Download_Enter script failed: %s", err.Error())
		callErrorScript("Download", stateExec)
		// No doStandaloneFailureStates here, since we have not done anything yet.
		return nil, err
	}
	installer, installers, err := installer.ReadHeaders(art, dt, key,
		device.stateScriptPath, &device.installerFactories)
	standaloneData := &standaloneData{
		installers: installers,
	}
	if err != nil {
		log.Errorf("Reading headers failed: %s", err.Error())
		callErrorScript("Download", stateExec)
		doStandaloneFailureStates(device, standaloneData, stateExec, false, false, true)
		return nil, err
	}

	standaloneData.artifactName = installer.GetArtifactName()

	err = installer.StorePayloads()
	if err != nil {
		log.Errorf("Download failed: %s", err.Error())
		callErrorScript("Download", stateExec)
		doStandaloneFailureStates(device, standaloneData, stateExec, false, false, true)
		return nil, err
	}
	err = stateExec.ExecuteAll("Download", "Leave", false, nil)
	if err != nil {
		log.Errorf("Download_Leave script failed: %s", err.Error())
		callErrorScript("Download", stateExec)
		doStandaloneFailureStates(device, standaloneData, stateExec, false, false, true)
		return nil, err
	}

	return standaloneData, nil
}

func doStandaloneInstallStates(art io.ReadCloser, key []byte,
	device *deviceManager, stateExec statescript.Executor) error {

	standaloneData, err := doStandaloneInstallStatesDownload(art, key, device, stateExec)
	if err != nil {
		return err
	}

	installers := standaloneData.installers

	rollbackSupport, err := determineRollbackSupport(installers)
	if err != nil {
		log.Error(err.Error())
		doStandaloneFailureStates(device, standaloneData, stateExec, false, false, true)
		return err
	}

	// ArtifactInstall state
	err = stateExec.ExecuteAll("ArtifactInstall", "Enter", false, nil)
	if err != nil {
		log.Errorf("ArtifactInstall_Enter script failed: %s", err.Error())
		callErrorScript("ArtifactInstall", stateExec)
		doStandaloneFailureStates(device, standaloneData, stateExec, true, true, true)
		return err
	}
	for _, inst := range installers {
		err = inst.InstallUpdate()
		if err != nil {
			log.Errorf("Installation failed: %s", err.Error())
			callErrorScript("ArtifactInstall", stateExec)
			doStandaloneFailureStates(device, standaloneData, stateExec, true, true, true)
			return err
		}
	}
	err = stateExec.ExecuteAll("ArtifactInstall", "Leave", false, nil)
	if err != nil {
		log.Errorf("ArtifactInstall_Leave script failed: %s", err.Error())
		callErrorScript("ArtifactInstall", stateExec)
		doStandaloneFailureStates(device, standaloneData, stateExec, true, true, true)
		return err
	}

	rebootNeeded, err := determineRebootNeeded(installers)
	if err != nil {
		doStandaloneFailureStates(device, standaloneData, stateExec, true, true, true)
		return err
	}

	err = standaloneStoreArtifactState(device.store, standaloneData.artifactName, installers)
	if err != nil {
		log.Errorf("Could not update database: %s", err.Error())
		return err
	}

	if rollbackSupport {
		fmt.Println("Use -commit to update, or -rollback to roll back the update.")
	} else {
		fmt.Println("Artifact doesn't support rollback. Committing immediately.")
		err = doStandaloneCommit(device, stateExec)
		if err != nil {
			return err
		}
	}

	if rebootNeeded {
		fmt.Println("At least one payload requested a reboot of the device it updated.")
	}

	return nil
}

func doStandaloneCommit(device *deviceManager, stateExec statescript.Executor) error {
	standaloneData, err := restoreStandaloneData(device)
	if err != nil {
		log.Errorf("Could not commit Artifact: %s", err.Error())
		return err
	}

	return doStandaloneCommitStates(device, standaloneData, stateExec)
}

func doStandaloneCommitStates(device *deviceManager, standaloneData *standaloneData,
	stateExec statescript.Executor) error {

	fmt.Println("Committing Artifact...")

	// ArtifactCommit state
	err := stateExec.ExecuteAll("ArtifactCommit", "Enter", false, nil)
	if err != nil {
		log.Errorf("ArtifactCommit_Enter script failed: %s", err.Error())
		callErrorScript("ArtifactCommit", stateExec)
		doStandaloneFailureStates(device, standaloneData, stateExec, true, true, true)
		return err
	}
	for _, inst := range standaloneData.installers {
		err = inst.CommitUpdate()
		if err != nil {
			log.Errorf("Commit failed: %s", err.Error())
			callErrorScript("ArtifactCommit", stateExec)
			doStandaloneFailureStates(device, standaloneData, stateExec, true, true, true)
			return err
		}
	}
	var errorToReturn error
	err = stateExec.ExecuteAll("ArtifactCommit", "Leave", false, nil)
	if err != nil {
		log.Errorf("ArtifactCommit_Leave script failed: %s", err.Error())
		callErrorScript("ArtifactCommit", stateExec)
		errorToReturn = err
		standaloneData.artifactName += brokenArtifactSuffix
		// Too late to roll back now. Continue.
	}

	err = doStandaloneCleanup(device, standaloneData, stateExec)
	if errorToReturn == nil {
		errorToReturn = err
	}

	return errorToReturn
}

func doStandaloneRollback(device *deviceManager, stateExec statescript.Executor) error {
	standaloneData, err := restoreStandaloneData(device)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	rollbackSupport, err := determineRollbackSupport(standaloneData.installers)
	if err != nil {
		log.Error(err.Error())
		return err
	} else if !rollbackSupport {
		return errors.New("No rollback support")
	}

	var firstErr error

	err = doStandaloneRollbackState(standaloneData, stateExec)
	if err != nil {
		firstErr = err
		log.Errorf("Error rolling back: %s", err.Error())
	}

	if firstErr == nil {
		// We rolled back successfully, keep old name.
		standaloneData.artifactName = ""
	} else {
		err = doStandaloneFailureStates(device, standaloneData, stateExec, false, true, false)
		if err != nil {
			log.Errorln(err.Error())
		}
	}

	err = doStandaloneCleanup(device, standaloneData, stateExec)
	if firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func doStandaloneRollbackState(standaloneData *standaloneData, stateExec statescript.Executor) error {
	fmt.Println("Rolling back Artifact...")

	var firstErr error

	err := stateExec.ExecuteAll("ArtifactRollback", "Enter", false, nil)
	if err != nil {
		firstErr = err
		log.Errorf("Error when executing ArtifactRollback_Enter scripts: %s", err.Error())
	}
	for _, inst := range standaloneData.installers {
		err = inst.Rollback()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			log.Errorf("Error when executing ArtifactRollback state: %s", err.Error())
		}
	}
	err = stateExec.ExecuteAll("ArtifactRollback", "Leave", false, nil)
	if err != nil {
		if firstErr == nil {
			firstErr = err
		}
		log.Errorf("Error when executing ArtifactFailure_Leave scripts: %s", err.Error())
	}

	return firstErr
}

func doStandaloneFailureStatesRollback(standaloneData *standaloneData,
	stateExec statescript.Executor) (bool, error) {

	var err error
	var rollbackSupport bool

	rollbackSupport, err = determineRollbackSupport(standaloneData.installers)
	if err != nil {
		log.Error(err.Error())
		// Continue with failure scripts anyway.
	} else if rollbackSupport {
		err = doStandaloneRollbackState(standaloneData, stateExec)
		if err != nil {
			log.Errorf("Error rolling back: %s", err.Error())
		}
		// Continue with failure scripts anyway.
	}

	return rollbackSupport, err
}

func doStandaloneFailureStatesFailure(standaloneData *standaloneData,
	stateExec statescript.Executor) error {

	var err error
	var firstErr error

	err = stateExec.ExecuteAll("ArtifactFailure", "Enter", true, nil)
	if err != nil {
		firstErr = err
		log.Errorf("Error when executing ArtifactFailure_Enter scripts: %s", err.Error())
	}
	for _, inst := range standaloneData.installers {
		err = inst.Failure()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			log.Errorf("Error when executing ArtifactFailure state: %s", err.Error())
		}
	}
	err = stateExec.ExecuteAll("ArtifactFailure", "Leave", true, nil)
	if err != nil {
		if firstErr == nil {
			firstErr = err
		}
		log.Errorf("Error when executing ArtifactFailure_Leave scripts: %s", err.Error())
	}

	return firstErr
}

func doStandaloneFailureStates(device *deviceManager, standaloneData *standaloneData,
	stateExec statescript.Executor, rollback, failure, cleanup bool) error {

	var firstErr error
	var rollbackSupport bool
	var err error

	if rollback {
		rollbackSupport, firstErr = doStandaloneFailureStatesRollback(standaloneData, stateExec)
	}

	if failure {
		err = doStandaloneFailureStatesFailure(standaloneData, stateExec)
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil || (failure && (!rollback || !rollbackSupport)) {
		standaloneData.artifactName += brokenArtifactSuffix
	} else if rollback || !failure {
		// Means keep old name.
		standaloneData.artifactName = ""
	}

	if cleanup {
		err = doStandaloneCleanup(device, standaloneData, stateExec)
		if firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func doStandaloneCleanup(device *deviceManager, standaloneData *standaloneData,
	stateExec statescript.Executor) error {

	var firstErr error

	for _, inst := range standaloneData.installers {
		err := inst.Cleanup()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			log.Errorf("Error when executing Cleanup state: %s", err.Error())
		}
	}

	err := device.store.WriteTransaction(func(txn store.Transaction) error {
		err := txn.Remove(datastore.StandaloneStateKey)
		if err != nil {
			return err
		}
		if standaloneData.artifactName != "" {
			return txn.WriteAll(datastore.ArtifactNameKey, []byte(standaloneData.artifactName))
		}
		return nil
	})
	if err != nil {
		if firstErr == nil {
			firstErr = err
		}
		log.Errorf("Could not update database: %s", err.Error())
	}

	return firstErr
}

func determineRollbackSupport(installers []installer.PayloadUpdatePerformer) (bool, error) {
	var support datastore.SupportsRollbackType
	for _, i := range installers {
		s, err := i.SupportsRollback()
		if err != nil {
			return false, err
		}
		if s {
			err = support.Set(datastore.RollbackSupported)
		} else {
			err = support.Set(datastore.RollbackNotSupported)
		}
		if err != nil {
			return false, err
		}
	}

	switch support {
	case datastore.RollbackSupported:
		return true, nil
	case datastore.RollbackNotSupported:
		return false, nil
	default:
		return false, errors.New("Rollback support could not be determined")
	}
}

func determineRebootNeeded(installers []installer.PayloadUpdatePerformer) (bool, error) {
	for _, i := range installers {
		needed, err := i.NeedsReboot()
		if err != nil {
			return false, err
		}
		if needed != installer.NoReboot {
			return true, nil
		}
	}
	return false, nil
}

func callErrorScript(state string, stateExec statescript.Executor) {
	err := stateExec.ExecuteAll(state, "Error", true, nil)
	if err != nil {
		log.Errorf("%s_Error script failed: %s", state, err.Error())
	}
}

func standaloneStoreArtifactState(store store.Store, artifactName string, installers []installer.PayloadUpdatePerformer) error {
	list := make([]string, len(installers))
	for c := range installers {
		list[c] = installers[c].GetType()
	}

	stateData := datastore.StandaloneStateData{
		Version:      datastore.StandaloneStateDataVersion,
		ArtifactName: artifactName,
		PayloadTypes: list,
	}

	data, err := json.Marshal(stateData)
	if err != nil {
		return err
	}

	return store.WriteAll(datastore.StandaloneStateKey, data)
}

func handlePreDatabaseRestore(device *deviceManager) (*standaloneData, error) {
	// Special exception for dual rootfs. Prior to Mender 2.0.0, we did not
	// store the standalone state in the database, so if we are upgrading
	// from pre-2.0.0, we need to accept that there is no database
	// entry. Instead use the bootloader detection that comes with the dual
	// rootfs module, which is the mechanism we used in the past.

	dualRootfs, ok := device.installerFactories.DualRootfs.(installer.DualRootfsDevice)

	// VerifyReboot() is what would be used to verify we have rebooted into
	// a new update.
	if !ok || dualRootfs.VerifyReboot() != nil {
		return nil, installer.ErrorNothingToCommit
	}

	// Forcibly sidestep the database for artifact name query and fetch it
	// directly from the artifact_info file. This was the way to get the
	// artifact name in the past. Normally we would call
	// GetCurrentArtifactName().
	name, err := getManifestData("artifact_name", device.artifactInfoFile)
	if err != nil {
		return nil, err
	}

	installers, err := installer.CreateInstallersFromList(&device.installerFactories,
		[]string{"rootfs-image"})
	if err != nil {
		return nil, err
	}

	return &standaloneData{
		artifactName: name,
		installers:   installers,
	}, nil
}

func restoreStandaloneData(device *deviceManager) (*standaloneData, error) {

	data, err := device.store.ReadAll(datastore.StandaloneStateKey)
	if os.IsNotExist(err) {
		return handlePreDatabaseRestore(device)
	} else if err != nil {
		return nil, err
	}
	var stateData datastore.StandaloneStateData
	err = json.Unmarshal(data, &stateData)
	if err != nil {
		return nil, err
	}

	switch stateData.Version {
	case datastore.StandaloneStateDataVersion:
		// Continue
	default:
		return nil, errors.New("Incompatible version stored in database.")
	}

	installers, err := installer.CreateInstallersFromList(&device.installerFactories,
		stateData.PayloadTypes)
	if err != nil {
		return nil, err
	}

	return &standaloneData{
		artifactName: stateData.ArtifactName,
		installers:   installers,
	}, nil
}

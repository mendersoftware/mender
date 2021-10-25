// Copyright 2021 Northern.tech AS
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

package app

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"github.com/mendersoftware/mender/client/api"
	"github.com/mendersoftware/mender/client/conf"
	"github.com/mendersoftware/mender/client/datastore"
	"github.com/mendersoftware/mender/client/installer"
	"github.com/mendersoftware/mender/client/statescript"
	"github.com/mendersoftware/mender/client/utils"
	"github.com/mendersoftware/mender/common/dbkeys"
	"github.com/mendersoftware/mender/common/store"
	"github.com/mendersoftware/mender/common/tls"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var (
	ErrorManualRebootRequired = errors.New("Manual reboot required")
)

type standaloneData struct {
	artifactName             string
	artifactGroup            string
	artifactTypeInfoProvides map[string]string
	artifactClearsProvides   []string
	installers               []installer.PayloadUpdatePerformer
}

// This will be run manually from command line ONLY
func DoStandaloneInstall(device *installer.DeviceManager, updateURI string,
	tlsConfig tls.Config, vKey []byte,
	stateExec statescript.Executor, rebootExitCode bool) error {

	var image io.ReadCloser
	var imageSize int64
	var err error
	var upclient api.Updater

	log.Debug("Starting device update.")

	var ar api.ApiRequester
	switch {
	case strings.HasPrefix(updateURI, "http:"):
		ar = &http.Client{}
		fallthrough
	case strings.HasPrefix(updateURI, "https:"):
		if ar == nil {
			ar, err = tls.NewHttpOrHttpsClient(tlsConfig)
			if err != nil {
				return errors.Wrap(err,
					"Can not initialize client for performing network update.")
			}
		}

		// we are having remote update
		log.Infof("Performing remote update from: [%s].", updateURI)

		upclient = api.NewUpdate()

		log.Debug("Client initialized. Start downloading image.")

		image, imageSize, err = upclient.FetchUpdate(ar, updateURI, 0)
		log.Debugf("Image downloaded: %d [%v] [%v]", imageSize, image, err)

	default:
		// perform update from local file
		log.Infof("Start updating from local image file: [%s]", updateURI)
		image, imageSize, err = installer.FetchUpdateFromFile(updateURI)

		log.Debugf("Fetching update from file results: [%v], %d, %v", image, imageSize, err)
	}

	if image == nil || err != nil {
		return errors.Wrapf(err, "Error while installing Artifact from command line")
	}
	defer image.Close()

	fmt.Fprintf(os.Stdout, "Installing Artifact of size %d...\n", imageSize)
	p := utils.NewProgressWriter(imageSize)
	tr := io.TeeReader(image, p)

	return doStandaloneInstallStates(ioutil.NopCloser(tr), vKey, device, stateExec, rebootExitCode)
}

func doStandaloneInstallStatesDownload(art io.ReadCloser, key []byte,
	device *installer.DeviceManager, stateExec statescript.Executor) (*standaloneData, error) {

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
		device.StateScriptPath, &device.InstallerFactories)
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
	standaloneData.artifactTypeInfoProvides, err = installer.GetArtifactProvides()
	if err != nil {
		return nil, err
	}
	if standaloneData.artifactTypeInfoProvides != nil {
		if _, ok := standaloneData.
			artifactTypeInfoProvides["artifact_name"]; ok {
			delete(standaloneData.artifactTypeInfoProvides,
				"artifact_name")
		}
		if grp, ok := standaloneData.
			artifactTypeInfoProvides["artifact_group"]; ok {
			standaloneData.artifactGroup = grp
			delete(standaloneData.artifactTypeInfoProvides,
				"artifact_group")
		}
	}
	depends, err := installer.GetArtifactDepends()
	if err != nil {
		return nil, err
	} else if depends != nil {
		currentProvides, err := datastore.LoadProvides(device.Store)
		if err != nil {
			return nil, err
		}
		if currentProvides, err = verifyAndSetArtifactNameInProvides(currentProvides, device.GetCurrentArtifactName); err != nil {
			log.Error(err.Error())
			return nil, err
		}
		if err = verifyArtifactDependencies(depends, currentProvides); err != nil {
			log.Error(err.Error())
			return nil, err
		}
	}

	standaloneData.artifactClearsProvides = installer.GetArtifactClearsProvides()

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
	device *installer.DeviceManager, stateExec statescript.Executor,
	rebootExitCode bool) error {

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

	err = storeStandaloneData(device.Store, standaloneData)
	if err != nil {
		log.Errorf("Could not update database: %s", err.Error())
		return err
	}

	if rollbackSupport {
		fmt.Println("Use 'commit' to update, or 'rollback' to roll back the update.")
	} else {
		fmt.Println("Artifact doesn't support rollback. Committing immediately.")
		err = DoStandaloneCommit(device, stateExec)
		if err != nil {
			return err
		}
	}

	if rebootNeeded {
		fmt.Println("At least one payload requested a reboot of the device it updated.")
		if rebootExitCode {
			return ErrorManualRebootRequired
		}
	}

	return nil
}

func DoStandaloneCommit(device *installer.DeviceManager, stateExec statescript.Executor) error {
	standaloneData, err := restoreStandaloneData(device)
	if err != nil {
		log.Errorf("Could not commit Artifact: %s", err.Error())
		return err
	}

	return doStandaloneCommitStates(device, standaloneData, stateExec)
}

func doStandaloneCommitStates(device *installer.DeviceManager, standaloneData *standaloneData,
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
		standaloneData.artifactName += conf.BrokenArtifactSuffix
		// Too late to roll back now. Continue.
	}

	err = doStandaloneCleanup(device, standaloneData, stateExec)
	if errorToReturn == nil {
		errorToReturn = err
	}

	return errorToReturn
}

func DoStandaloneRollback(device *installer.DeviceManager, stateExec statescript.Executor) error {
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

func doStandaloneFailureStates(device *installer.DeviceManager, standaloneData *standaloneData,
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
		standaloneData.artifactName += conf.BrokenArtifactSuffix
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

func doStandaloneCleanup(device *installer.DeviceManager, standaloneData *standaloneData,
	_ statescript.Executor) error {

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

	err := device.Store.WriteTransaction(func(txn store.Transaction) error {
		err := txn.Remove(dbkeys.StandaloneStateKey)
		if err != nil {
			return err
		}
		if standaloneData.artifactName != "" {
			return datastore.CommitArtifactData(txn, standaloneData.artifactName,
				standaloneData.artifactGroup, standaloneData.artifactTypeInfoProvides,
				standaloneData.artifactClearsProvides)
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

// storeStandaloneData stores uncommitted Artifact payload data, so that the
// client is able to retrieve this information across reboots, in case the
// update-module in question requests such.
func storeStandaloneData(store store.Store, sd *standaloneData) error {
	installers := sd.installers
	list := make([]string, len(installers))
	for c := range installers {
		list[c] = installers[c].GetType()
	}

	stateData := datastore.StandaloneStateData{
		Version:                  datastore.StandaloneStateDataVersion,
		ArtifactName:             sd.artifactName,
		ArtifactGroup:            sd.artifactGroup,
		ArtifactTypeInfoProvides: sd.artifactTypeInfoProvides,
		ArtifactClearsProvides:   sd.artifactClearsProvides,
		PayloadTypes:             list,
	}

	data, err := json.Marshal(stateData)
	if err != nil {
		return err
	}

	return store.WriteAll(dbkeys.StandaloneStateKey, data)
}

func handlePreDatabaseRestore(device *installer.DeviceManager) (*standaloneData, error) {
	// Special exception for dual rootfs. Prior to Mender 2.0.0, we did not
	// store the standalone state in the database, so if we are upgrading
	// from pre-2.0.0, we need to accept that there is no database
	// entry. Instead use the bootloader detection that comes with the dual
	// rootfs module, which is the mechanism we used in the past.

	dualRootfs, ok := device.InstallerFactories.DualRootfs.(installer.DualRootfsDevice)

	// VerifyReboot() is what would be used to verify we have rebooted into
	// a new update.
	if !ok || dualRootfs.VerifyReboot() != nil {
		return nil, installer.ErrorNothingToCommit
	}

	// Forcibly sidestep the database for artifact name query and fetch it
	// directly from the artifact_info file. This was the way to get the
	// artifact name in the past. Normally we would call
	// GetCurrentArtifactName().
	name, err := installer.GetManifestData("artifact_name", device.ArtifactInfoFile)
	if err != nil {
		return nil, err
	}

	installers, err := installer.CreateInstallersFromList(&device.InstallerFactories,
		[]string{"rootfs-image"})
	if err != nil {
		return nil, err
	}

	return &standaloneData{
		artifactName: name,
		installers:   installers,
	}, nil
}

// restoreStandaloneData retrieves Artifact payload data from the database, and
// is the inverse function of `storeStandaloneData`, meaning that it retrieves
// Artifact payload data from the database after a reboot.
func restoreStandaloneData(device *installer.DeviceManager) (*standaloneData, error) {

	data, err := device.Store.ReadAll(dbkeys.StandaloneStateKey)
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

	installers, err := installer.CreateInstallersFromList(&device.InstallerFactories,
		stateData.PayloadTypes)
	if err != nil {
		return nil, err
	}

	return &standaloneData{
		artifactName:             stateData.ArtifactName,
		artifactGroup:            stateData.ArtifactGroup,
		artifactTypeInfoProvides: stateData.ArtifactTypeInfoProvides,
		artifactClearsProvides:   stateData.ArtifactClearsProvides,
		installers:               installers,
	}, nil
}

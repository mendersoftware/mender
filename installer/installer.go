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

package installer

import (
	"fmt"
	"io"
	"os"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mendersoftware/mender-artifact/areader"
	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/mendersoftware/mender/statescript"
)

type Rebooter interface {
	Reboot() error
}

type PayloadUpdatePerformer interface {
	Rebooter
	handlers.UpdateStorer
	InstallUpdate() error
	NeedsReboot() (RebootAction, error)
	CommitUpdate() error
	SupportsRollback() (bool, error)
	Rollback() error
	// Verify that rebooting into the new update worked.
	VerifyReboot() error
	RollbackReboot() error
	// Verify that rebooting into the old update worked.
	VerifyRollbackReboot() error
	Failure() error
	Cleanup() error

	GetType() string
}

type AllModules struct {
	// Built-in module.
	DualRootfs handlers.UpdateStorerProducer
	// External modules.
	Modules *ModuleInstallerFactory
}

type ArtifactInfoGetter interface {
	GetCurrentArtifactName() (string, error)
	GetCurrentArtifactGroup() (string, error)
}

type DeviceInfoGetter interface {
	GetDeviceType() (string, error)
}

type Installer struct {
	ar *areader.Reader
}

type RebootAction int

const (
	NoReboot = iota
	RebootRequired
	AutomaticReboot
)

var (
	ErrorNothingToCommit = errors.New("There is nothing to commit")
)

func Install(art io.ReadCloser, dt string, key []byte, scrDir string,
	inst *AllModules) ([]PayloadUpdatePerformer, error) {

	installer, payloads, err := ReadHeaders(art, dt, key, scrDir, inst)
	if err != nil {
		return payloads, err
	}

	err = installer.StorePayloads()
	return payloads, err
}

func ReadHeaders(art io.ReadCloser, dt string, key []byte, scrDir string,
	inst *AllModules) (*Installer, []PayloadUpdatePerformer, error) {

	var ar *areader.Reader
	var installers []PayloadUpdatePerformer
	var err error

	// if there is a verification key artifact must be signed
	if key != nil {
		ar = areader.NewReaderSigned(art)
	} else {
		ar = areader.NewReader(art)
		log.Info("No public key was provided for authenticating the artifact")
	}

	// Important for the client to forbid artifacts types we don't know.
	ar.ForbidUnknownHandlers = true

	if err = registerHandlers(ar, inst); err != nil {
		return nil, installers, err
	}

	ar.CompatibleDevicesCallback = func(devices []string) error {
		log.Debugf("Checking if device [%s] is on compatible device list: %v\n",
			dt, devices)
		if dt == "" {
			log.Errorf("Unknown device_type. Continuing with update")
			return nil
		}
		for _, dev := range devices {
			if dev == dt {
				return nil
			}
		}
		return errors.Errorf("installer: image (device types %v) not compatible with device %v",
			devices, dt)
	}

	// VerifySignatureCallback needs to be registered both for
	// NewReader and NewReaderSigned to print a warning if artifact is signed
	// but no verification key is provided.
	ar.VerifySignatureCallback = func(message, sig []byte) error {
		// MEN-1196 skip verification of the signature if there is no key
		// provided. This means signed artifact will be installed on all
		// devices having no key specified.
		if key == nil {
			log.Warn("Installer: Installing signed artifact without verification " +
				"as verification key is missing")
			return nil
		}

		// Do the verification only if the key is provided.
		s, err := artifact.NewPKIVerifier(key)
		if err != nil {
			return err
		}
		err = s.Verify(message, sig)
		if err == nil {
			// MEN-2152 Provide confirmation in log that digital signature was authenticated.
			log.Info("Installer: authenticated digital signature of artifact")
		}
		return err
	}

	scr := statescript.NewStore(scrDir)
	// we need to wipe out the scripts directory first
	if err = scr.Clear(); err != nil {
		log.Errorf("Installer: Error initializing directory for scripts [%s]: %v",
			scrDir, err)
		return nil, installers, errors.Wrap(
			err,
			"installer: error initializing directory for scripts",
		)
	}

	// All the scripts that are part of the artifact will be processed here.
	ar.ScriptsReadCallback = func(r io.Reader, fi os.FileInfo) error {
		log.Debugf("Installer: Processing script: %s", fi.Name())
		return scr.StoreScript(r, fi.Name())
	}

	// read the artifact
	if err = ar.ReadArtifactHeaders(); err != nil {
		return nil, installers, errors.Wrap(err, "installer: failed to read Artifact")
	}

	if err = scr.Finalize(ar.GetInfo().Version); err != nil {
		return nil, installers, errors.Wrap(err, "installer: error finalizing writing scripts")
	}

	updateStorers, err := ar.GetUpdateStorers()
	if err != nil {
		return nil, installers, err
	}

	// Remove this when adding support for more than one payload.
	if len(updateStorers) > 1 {
		return nil, installers, errors.New(
			"Artifacts with more than one payload are not supported yet!",
		)
	}

	installers, err = getInstallerList(updateStorers)
	if err != nil {
		return nil, installers, err
	}

	log.Debugf(
		"Installer: Successfully read artifact [name: %v; version: %v; compatible devices: %v]",
		ar.GetArtifactName(), ar.GetInfo().Version, ar.GetCompatibleDevices())

	return &Installer{ar}, installers, nil
}

func (i *Installer) StorePayloads() error {
	return i.ar.ReadArtifactData()
}

func (i *Installer) GetArtifactName() string {
	return i.ar.GetArtifactName()
}

// Returns a list of compatible devices
func (i *Installer) GetCompatibleDevices() []string {
	return i.ar.GetCompatibleDevices()
}

// Returns the merged artifact provides header-info and type-info fields
// for artifact version >= 3. Returns nil if version < 3
func (i *Installer) GetArtifactProvides() (map[string]string, error) {
	return i.ar.MergeArtifactProvides()
}

// Returns the merged artifact depends header-info and type-info fields
// for artifact version >= 3. Returns nil if version < 3
func (i *Installer) GetArtifactDepends() (map[string]interface{}, error) {
	return i.ar.MergeArtifactDepends()
}

// Returns all `clears_artifact_depends` fields from all payloads.
func (i *Installer) GetArtifactClearsProvides() []string {
	return i.ar.MergeArtifactClearsProvides()
}

func registerHandlers(ar *areader.Reader, inst *AllModules) error {

	// Built-in rootfs handler.
	if inst.DualRootfs != nil {
		rootfs := handlers.NewRootfsInstaller()
		rootfs.SetUpdateStorerProducer(inst.DualRootfs)
		if err := ar.RegisterHandler(rootfs); err != nil {
			return errors.Wrap(err, "failed to register rootfs install handler")
		}
	}

	if inst.Modules == nil {
		return nil
	}

	// Update modules.
	updateTypes := inst.Modules.GetModuleTypes()
	for _, updateType := range updateTypes {
		if updateType == "rootfs-image" {
			log.Errorf("Found update module called %s, which "+
				"cannot be overridden. Ignoring.", updateType)
			continue
		}
		moduleImage := handlers.NewModuleImage(updateType)
		moduleImage.SetUpdateStorerProducer(inst.Modules)
		if err := ar.RegisterHandler(moduleImage); err != nil {
			return errors.Wrapf(err, "failed to register '%s' install handler",
				updateType)
		}
	}

	return nil
}

func getInstallerList(updateStorers []handlers.UpdateStorer) ([]PayloadUpdatePerformer, error) {
	var list []PayloadUpdatePerformer
	for _, us := range updateStorers {
		installer, ok := us.(PayloadUpdatePerformer)
		if !ok {
			// If the installer does not implement PayloadUpdatePerformer interface, it means that
			// it is an Artifact with no payload (for instance, bootstrap Artifact). Just skip.
			continue
		}
		list = append(list, installer)
	}

	return list, nil
}

func CreateInstallersFromList(inst *AllModules,
	desiredTypes []string) ([]PayloadUpdatePerformer, error) {

	payloadStorers := make([]handlers.UpdateStorer, len(desiredTypes))
	typesFromDisk := inst.Modules.GetModuleTypes()

	for n, desired := range desiredTypes {
		var err error
		if desired == "rootfs-image" {
			if inst.DualRootfs != nil {
				payloadStorers[n], err = inst.DualRootfs.NewUpdateStorer(&desired, n)
				if err != nil {
					return nil, err
				}
			} else {
				log.Error("Dual rootfs configuration not found when resuming update. " +
					"Recovery may fail.")
				payloadStorers[n] = NewStubInstaller(desired)
			}
			continue
		}

		found := false
		for _, fromDisk := range typesFromDisk {
			if fromDisk == desired {
				found = true
				break
			}
		}
		if found {
			payloadStorers[n], err = inst.Modules.NewUpdateStorer(&desired, n)
			if err != nil {
				return nil, err
			}
		} else {
			log.Errorf("Update module %s not found when assembling list of "+
				"update modules. Recovery may fail.", desired)
			payloadStorers[n] = NewStubInstaller(desired)
		}
	}

	return getInstallerList(payloadStorers)
}

// FetchUpdateFromFile returns a byte stream of the given file, size of the file
// and an error if one occurred.
func FetchUpdateFromFile(file string) (io.ReadCloser, int64, error) {
	fd, err := os.Open(file)
	if err != nil {
		return nil, 0, fmt.Errorf("Not able to open image file: %s: %s\n",
			file, err.Error())
	}

	imageInfo, err := fd.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("Unable to stat() file: %s: %s\n", file, err.Error())
	}

	return fd, imageInfo.Size(), nil
}

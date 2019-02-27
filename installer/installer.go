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

package installer

import (
	"archive/tar"
	"encoding/hex"
	"io"
	"io/ioutil"
	"os"

	"github.com/itchio/savior"
	"github.com/itchio/savior/gzipsource"
	"github.com/itchio/savior/seeksource"
	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender-artifact/areader"
	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/mendersoftware/mender/statescript"
	"github.com/pkg/errors"
)

type InstallationStateStore interface {
	ReadAll(name string) ([]byte, error)
	WriteAll(name string, data []byte) error
	Remove(name string) error
	Close() error
}

// InstallationProgressCallback is a function which will be called each time a block of bytes is persisted to the destination storage.
// When this function is called, bytes blockStartByte through blockEndByte-1 have been persisted to stable storage.
type InstallationProgressConsumer interface {
	UpdateInstallationProgress(blockStartByte, blockEndByte int64)
}

type UInstaller interface {
	InstallUpdate(r io.ReadCloser, updateSize int64, initialOffset int64, ipc InstallationProgressConsumer) error
	VerifyUpdatedPartition(size int64, expectedSHA256Checksum []byte) error
	EnableUpdatedPartition() error
}

// CheckpointConsumer implements both the savior.SourceSaveConsumer interface InstallationProgressConsumer interfaces.  It consumes both and writes the savior.SourceCheckpoint into the InstallationStateStore provided to NewCheckpointConsumer
type CheckpointConsumer struct {
	installationStateStore InstallationStateStore

	prefix string // prefix attached to all WriteAll() calls to ISS

	source              savior.Source
	checkpointInWaiting *savior.SourceCheckpoint // most recently-received checkpoint from source
	lastByteWritten     int64                    // one past the last byte written to stable storage

	doneRecordingCheckpoints bool // when this is true we stop persisting checkpoints
}

func NewCheckpointConsumer(source savior.Source, prefix string, installationStateStore InstallationStateStore) *CheckpointConsumer {
	return &CheckpointConsumer{
		source:                   source,
		prefix:                   prefix,
		installationStateStore:   installationStateStore,
		doneRecordingCheckpoints: false,
	}
}

func (cc *CheckpointConsumer) maybeCommitCheckpoint() {
	if !cc.doneRecordingCheckpoints {
		if cc.checkpointInWaiting != nil {
			if cc.checkpointInWaiting.Offset < cc.lastByteWritten {
				// ready to commit this checkpoint.
				ckpt := cc.checkpointInWaiting
				cc.checkpointInWaiting = nil

				log.Infof("committing checkpoint for offset %d", ckpt.Offset)

				gobBytes, err := CheckpointToGob(ckpt)
				if err != nil {
					log.Errorf("Failed to marshal checkpoint at offset %d: %v", ckpt.Offset, err)
				}

				err = cc.installationStateStore.WriteAll(
					cc.prefix+"checkpoint",
					gobBytes,
				)

				if err != nil {
					log.Errorf("Failed to save checkpoint at offset %d: %v", ckpt.Offset, err)
				}

			}
		}
	} else {
		// Throw away checkpoint
		cc.checkpointInWaiting = nil
	}
}

func (cc *CheckpointConsumer) UpdateInstallationProgress(blockStartByte, blockEndByte int64) {
	cc.lastByteWritten = blockEndByte
	cc.maybeCommitCheckpoint()
}

func (cc *CheckpointConsumer) Save(ckpt *savior.SourceCheckpoint) error {
	if ckpt.Offset > 0 {
		cc.checkpointInWaiting = ckpt
		cc.maybeCommitCheckpoint()
	}

	cc.source.WantSave() // always ask for another checkpoint

	return nil
}

func (cc *CheckpointConsumer) ClearAndStopRecordingCheckpoints() error {
	cc.doneRecordingCheckpoints = true
	cc.checkpointInWaiting = nil

	err := cc.installationStateStore.Remove(cc.prefix + "checkpoint")
	if err != nil && os.IsNotExist(err) {
		err = nil
	}
	return err
}

func Install(art io.ReadCloser, size int64, dt string, key []byte, scrDir string,
	device UInstaller, acceptStateScripts bool,
	installationStateStore InstallationStateStore) error {

	// Remember: "size" here is the size of the entire update artifact, not the rootfs-image

	newOuterTarReaderFn := areader.DefaultNewOuterTarReaderish
	newInnerTarReaderFn := areader.DefaultNewInnerTarReaderish
	newGzipReaderFn := areader.DefaultNewGzipReaderish
	newReaderChecksumFn := areader.DefaultNewReaderCheckSumish

	sharedState := &struct {
		initialOffsetBytes        int64
		shouldEnableCheckpointing bool
		checkpointConsumer        *CheckpointConsumer
	}{
		initialOffsetBytes:        0,
		shouldEnableCheckpointing: false,
		checkpointConsumer:        nil,
	}

	// If installationStateStore is non-nil and art is ReadSeeker, then we can enable checkpointing.

	if installationStateStore != nil {

		rs, ok := art.(io.ReadSeeker)
		if ok {
			sharedState.shouldEnableCheckpointing = true

			// Disable artifact checksum verification in areader.Reader.  We
			// still verify the on-device (readback) checksum so there should
			// be no concerns about integrity.
			// TODO: Reinstate this?
			newReaderChecksumFn = nil
			log.Infof("checkpointing is enabled, artifact reader checksum verification is disabled, will verify artifact checksum via device readback upon completion of installation")

			//  Inject checkpointable savior.Sources
			newOuterTarReaderFn = func(r io.Reader) (areader.TarReaderish, error) {

				seeksrc := seeksource.NewWithSize(rs, size)
				_, err := seeksrc.Resume(nil)
				if err != nil {
					return nil, err
				}

				tfs, err := NewTarFileSource(seeksrc)
				if err != nil {
					return nil, err
				}
				_, err = tfs.Resume(nil)
				if err != nil {
					return nil, err
				}
				return tfs, nil
			}

			newGzipReaderFn = func(r io.Reader) (areader.GzipReaderish, error) {
				upstreamSource, ok := r.(savior.Source)
				if !ok {
					return nil, errors.New("logic error: checkpointing is enabled but upstream reader is not a savior.Source, cannot create gzip reader")
				}

				gss := gzipsource.New(upstreamSource)
				_, err := gss.Resume(nil)
				if err != nil {
					return nil, errors.Wrap(err, "unable to create gzipsource")
				}

				return gss, nil
			}

			newInnerTarReaderFn = func(r io.Reader, updateNumber int, manifest *artifact.ChecksumStore) (areader.TarReaderish, *tar.Header, int64, error) {
				upstreamSource, ok := r.(savior.Source)
				if !ok {
					return nil, nil, 0, errors.New("logic error: checkpointing is enabled but upstream reader is not a savior.Source, cannot create inner tar reader")
				}

				tfs, err := NewTarFileSource(upstreamSource)
				if err != nil {
					return nil, nil, 0, err
				}

				_, err = tfs.Resume(nil)
				if err != nil {
					return nil, nil, 0, errors.Wrap(err, "unable to create inner tar reader")
				}

				var checkpoint *savior.SourceCheckpoint

				checkpointKeyName := "checkpoint"

				if checkpointBytes, err := installationStateStore.ReadAll(checkpointKeyName); err == nil {
					// Try to unmarshal checkpoint
					checkpoint, err = GobToCheckpoint(checkpointBytes)
					if err != nil {
						log.Errorf("failed to unmarshal saved installation checkpoint: %v", err)
						checkpoint = nil

						// Clear the checkpoint since it's no good
						err = installationStateStore.Remove("checkpoint")
						if err != nil {
							log.Errorf("failed to clear bogus checkpoint: %v", err)
						} else {
							log.Info("successfully cleared bogus checkpoint")
						}
					}
				}

				ckptConsumer := NewCheckpointConsumer(tfs, "", installationStateStore)
				tfs.SetSourceSaveConsumer(ckptConsumer)
				sharedState.checkpointConsumer = ckptConsumer

				_, err = tfs.Resume(checkpoint)
				if err != nil {
					return nil, nil, 0, err
				}

				var hdr *tar.Header
				var offset int64
				if checkpoint != nil {
					offset = tfs.GetCurrentOffset()
					hdr = tfs.GetCurrentHeader()
					sharedState.initialOffsetBytes = offset
					log.Infof("resuming installation from checkpoint at %d bytes...", offset)
				}

				tfs.WantSave()

				return tfs, hdr, offset, err
			}
		}

	}

	if !sharedState.shouldEnableCheckpointing {
		log.Infof("checkpointing is disabled")
	}

	rootfs := handlers.NewRootfsInstaller()

	rootfs.InstallHandler = func(r io.Reader, df *handlers.DataFile) error {
		// Remember: here, df.Size is the size of the root-image
		log.Debugf("installing update %v of size %v", df.Name, df.Size)

		// check shouldEnableCheckpointing here and if so, we should be able to
		// cast r into a TarFileSource which gives us an inital offset to pass
		// to the device

		initialOffsetBytes := sharedState.initialOffsetBytes
		log.Infof("install handler resuming installation at %d bytes", initialOffsetBytes)

		ckptConsumer := sharedState.checkpointConsumer

		err := device.InstallUpdate(ioutil.NopCloser(r), df.Size, initialOffsetBytes, ckptConsumer)
		if err != nil {
			log.Errorf("update image installation failed: %v", err)
			return err
		}

		// Clear checkpoint state here, so that if verification fails below we don't just keep trying from the last checkpoint.
		if ckptConsumer != nil {
			err = ckptConsumer.ClearAndStopRecordingCheckpoints()
			if err != nil {
				log.Errorf("after successful InstallUpdate, failed to clear checkpoint state: %v", err)
				// not sure what to do if this fails -- just move on.
			}
		}

		// Note: df.Checksum is of type []byte, but actually contains the ASCII-encoded hex value of the checksum.
		// VerifyUpdatedPartition wants the []byte to contain the bytes of the checksum, so we hex.DecodeString() here.
		checksumString := string(df.Checksum)
		checksumAsBytes, err := hex.DecodeString(checksumString)
		if err != nil {
			log.Errorf("unable to interpret checksum string \"%s\" as bytes: %v", checksumString, checksumAsBytes)
			return err
		}

		err = device.VerifyUpdatedPartition(df.Size, checksumAsBytes)
		if err != nil {
			log.Errorf("update image installation failed verification check: %v", err)
			return err
		}

		return nil
	}

	var ar *areader.Reader
	// if there is a verification key artifact must be signed
	ar = areader.NewReaderCustom(
		art,
		newOuterTarReaderFn,
		newInnerTarReaderFn,
		newGzipReaderFn,
		newReaderChecksumFn,
		(key != nil), //isSigned
	)

	if key == nil {
		log.Info("no public key was provided for authenticating the artifact")
	}

	if err := ar.RegisterHandler(rootfs); err != nil {
		return errors.Wrap(err, "failed to register install handler")
	}

	ar.CompatibleDevicesCallback = func(devices []string) error {
		log.Debugf("checking if device [%s] is on compatibile device list: %v",
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
			log.Warn("installer: installing signed artifact without verification " +
				"as verification key is missing")
			return nil
		}

		// Do the verification only if the key is provided.
		s := artifact.NewVerifier(key)
		err := s.Verify(message, sig)
		if err == nil {
			// MEN-2152 Provide confirmation in log that digital signature was authenticated.
			log.Info("installer: authenticated digital signature of artifact")
		}
		return err
	}

	scr := statescript.NewStore(scrDir)
	// we need to wipe out the scripts directory first
	if err := scr.Clear(); err != nil {
		log.Errorf("installer: error initializing directory for scripts [%s]: %v",
			scrDir, err)
		return errors.Wrap(err, "installer: error initializing directory for scripts")
	}

	if acceptStateScripts {
		// All the scripts that are part of the artifact will be processed here.
		ar.ScriptsReadCallback = func(r io.Reader, fi os.FileInfo) error {
			log.Debugf("installer: processing script: %s", fi.Name())
			return scr.StoreScript(r, fi.Name())
		}
	} else {
		ar.ScriptsReadCallback = func(r io.Reader, fi os.FileInfo) error {
			errMsg := "will not install artifact with state-scripts when installing from cmd-line. Use -f to override"
			return errors.New(errMsg)
		}
	}

	// read the artifact
	if err := ar.ReadArtifact(); err != nil {
		return errors.Wrap(err, "installer: failed to read and install update")
	}

	if err := scr.Finalize(ar.GetInfo().Version); err != nil {
		return errors.Wrap(err, "installer: error finalizing writing scripts")
	}

	log.Debugf(
		"installer: successfully read artifact [name: %v; version: %v; compatible devices: %v]",
		ar.GetArtifactName(), ar.GetInfo().Version, ar.GetCompatibleDevices())

	return nil
}

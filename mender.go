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
	"bufio"
	"os"
	"strings"
	"time"

	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
)

type Controller interface {
	Authorize() menderError
	Bootstrap() menderError
	GetCurrentImageID() string
	GetUpdatePollInterval() time.Duration
	HasUpgrade() (bool, menderError)
	CheckUpdate() (*UpdateResponse, menderError)

	UInstallCommitRebooter
	Updater
	StateRunner
}

const (
	defaultManifestFile = "/etc/build_mender"
	defaultKeyFile      = "mender-agent.pem"
	defaultDataStore    = "/var/lib/mender"
)

type MenderState int

// State transitions:
//
//            unknown
//               |
//               v
//             init
//               |
//               v
//          bootstrapped
//               |
//       +-------+-------------+
//       |                     |
//       v                     v
// fresh update         wait for update
//
// Any state can transition to MenderStateError

const (
	// initial state
	MenderStateInit MenderState = iota
	// client is bootstrapped, i.e. ready to go
	MenderStateBootstrapped
	// wait for new update
	MenderStateUpdateCheckWait
	// check update
	MenderStateUpdateCheck
	// update fetch
	MenderStateUpdateFetch
	// update install
	MenderStateUpdateInstall
	// commit needed
	MenderStateUpdateCommit
	// reboot
	MenderStateReboot
	// error
	MenderStateError
	// exit state
	MenderStateDone
)

type mender struct {
	UInstallCommitRebooter
	Updater
	env            BootEnvReadWriter
	state          State
	config         menderConfig
	manifestFile   string
	deviceKey      *Keystore
	forceBootstrap bool
}

type MenderPieces struct {
	updater Updater
	device  UInstallCommitRebooter
	env     BootEnvReadWriter
	store   Store
}

func NewMender(config menderConfig, pieces MenderPieces) *mender {

	ks := NewKeystore(pieces.store)
	if ks == nil {
		return nil
	}

	m := &mender{
		UInstallCommitRebooter: pieces.device,
		Updater:                pieces.updater,
		env:                    pieces.env,
		manifestFile:           defaultManifestFile,
		deviceKey:              ks,
		state:                  initState,
		config:                 config,
	}

	if err := m.deviceKey.Load(m.config.DeviceKey); err != nil && IsNoKeys(err) == false {
		log.Errorf("failed to load device keys: %s", err)
		return nil
	}

	return m
}

func (m mender) GetCurrentImageID() string {
	// This is where Yocto stores buid information
	manifest, err := os.Open(m.manifestFile)
	if err != nil {
		log.Error("Can not read current image id.")
		return ""
	}

	imageID := ""

	scanner := bufio.NewScanner(manifest)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		log.Debug("Read data from device manifest file: ", line)
		if strings.HasPrefix(line, "IMAGE_ID") {
			log.Debug("Found device id line: ", line)
			lineID := strings.Split(line, "=")
			if len(lineID) != 2 {
				log.Errorf("Broken device manifest file: (%v)", lineID)
				return ""
			}
			log.Debug("Current image id: ", strings.TrimSpace(lineID[1]))
			return strings.TrimSpace(lineID[1])
		}
	}
	if err := scanner.Err(); err != nil {
		log.Error(err)
	}
	return imageID
}

func (m *mender) HasUpgrade() (bool, menderError) {
	env, err := m.env.ReadEnv("upgrade_available")
	if err != nil {
		return false, NewFatalError(err)
	}
	upgradeAvailable := env["upgrade_available"]

	// we are after update
	if upgradeAvailable == "1" {
		return true, nil
	}
	return false, nil
}

func (m *mender) ForceBootstrap() {
	m.forceBootstrap = true
}

func (m *mender) needsBootstrap() bool {
	if m.forceBootstrap {
		return true
	}

	if m.deviceKey.Private() == nil {
		log.Debugf("needs keys")
		return true
	}

	return false
}

func (m *mender) Bootstrap() menderError {
	if !m.needsBootstrap() {
		return nil
	}

	return m.doBootstrap()
}

func (m *mender) doBootstrap() menderError {
	if m.deviceKey.Private() == nil {
		log.Infof("device keys not present, generating")
		if err := m.deviceKey.Generate(); err != nil {
			return NewFatalError(err)
		}

		if err := m.deviceKey.Save(m.config.DeviceKey); err != nil {
			log.Errorf("failed to save keys to %s: %s",
				m.config.DeviceKey, err)
			return NewFatalError(err)
		}
	}

	m.forceBootstrap = false

	return nil
}

// Check if new update is available. In case of errors, returns nil and error
// that occurred. If no update is available *UpdateResponse is nil, otherwise it
// contains update information.
func (m *mender) CheckUpdate() (*UpdateResponse, menderError) {
	currentImageID := m.GetCurrentImageID()
	//TODO: if currentImageID == "" {
	// 	return errors.New("")
	// }

	haveUpdate, err := m.Updater.GetScheduledUpdate(m.config.ServerURL, m.config.DeviceID)
	if err != nil {
		log.Error("Error receiving scheduled update data: ", err)
		return nil, NewTransientError(err)
	}

	log.Debug("Received correct response for update request.")

	update, ok := haveUpdate.(UpdateResponse)
	if !ok {
		return nil, NewTransientError(errors.Errorf("not an update response?"))
	}

	if update.Image.YoctoID == currentImageID {
		return nil, nil
	}
	return &update, nil
}

func (m mender) GetUpdatePollInterval() time.Duration {
	return time.Duration(m.config.PollIntervalSeconds) * time.Second
}

func (m *mender) SetState(s State) {
	log.Infof("Mender state: %v -> %v", m.state.Id(), s.Id())
	m.state = s
}

func (m *mender) GetState() State {
	return m.state
}

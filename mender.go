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
)

type Controller interface {
	Bootstrap() error
	TransitionState() MenderState
	GetCurrentImageID() string
	GetUpdatePollInterval() time.Duration
	LastError() error
	HasUpgrade() (bool, error)

	UInstallCommitRebooter
	Updater
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
// Any state can transition to MenderStateError, setting LastError()
// to the error that triggered the transition

const (
	MenderStateUnknown MenderState = iota
	// initial state
	MenderStateInit
	// client is bootstrapped, i.e. ready to go
	MenderStateBootstrapped
	// update applied, waiting for commit
	MenderStateRunningWithFreshUpdate
	// wait for new update
	MenderStateWaitForUpdate
	// error occurred, call Controller.LastError() to obtain the
	// error
	MenderStateError
)

type mender struct {
	UInstallCommitRebooter
	Updater
	env            BootEnvReadWriter
	state          MenderState
	config         menderConfig
	manifestFile   string
	deviceKey      *Keystore
	forceBootstrap bool
	lastError      error
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
		config:                 config,
	}

	if err := m.deviceKey.Load(m.config.DeviceKey); err != nil && IsNoKeys(err) == false {
		log.Errorf("failed to load device keys: %s", err)
		return nil
	}

	return m
}

func (m *mender) TransitionState() MenderState {
	m.updateState()
	return m.state
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

func (m *mender) changeState(state MenderState) {
	log.Infof("Mender state: %v -> %v", m.state, state)
	m.state = state
}

func (m *mender) HasUpgrade() (bool, error) {
	env, err := m.env.ReadEnv("upgrade_available")
	if err != nil {
		return false, err
	}
	upgradeAvailable := env["upgrade_available"]

	// we are after update
	if upgradeAvailable == "1" {
		return true, nil
	}
	return false, nil
}

func (m *mender) updateState() {

	newstate := MenderStateUnknown
	var merr error

	switch m.state {
	case MenderStateInit:
		if m.needsBootstrap() {
			if err := m.doBootstrap(); err != nil {
				newstate = MenderStateError
				merr = err
			} else {
				newstate = MenderStateBootstrapped
			}
		} else {
			newstate = MenderStateBootstrapped
		}
	case MenderStateBootstrapped:
		upg, err := m.HasUpgrade()
		if err != nil {
			newstate = MenderStateError
			merr = err
		} else {
			if upg {
				newstate = MenderStateRunningWithFreshUpdate
			} else {
				newstate = MenderStateWaitForUpdate
			}
		}
	}

	// record last errpr
	if newstate == MenderStateError {
		m.lastError = merr
	}
	m.changeState(newstate)
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

func (m *mender) Bootstrap() error {
	if !m.needsBootstrap() {
		return nil
	}

	return m.doBootstrap()
}

func (m *mender) doBootstrap() error {
	if m.deviceKey.Private() == nil {
		log.Infof("device keys not present, generating")
		if err := m.deviceKey.Generate(); err != nil {
			return err
		}

		if err := m.deviceKey.Save(m.config.DeviceKey); err != nil {
			log.Errorf("faiiled to save keys to %s: %s",
				m.config.DeviceKey, err)
			return err
		}
	}

	m.forceBootstrap = false

	return nil
}

func (m *mender) LastError() error {
	return m.lastError
}

func (m mender) GetUpdatePollInterval() time.Duration {
	return time.Duration(m.config.PollIntervalSeconds) * time.Second
}

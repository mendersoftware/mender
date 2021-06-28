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
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mendersoftware/mender/conf"
	"github.com/mendersoftware/mender/datastore"
	"github.com/mendersoftware/mender/dbus"
	"github.com/mendersoftware/mender/store"
	"github.com/mendersoftware/mender/system"
)

// Config section

type MenderDaemon struct {
	AuthManager          AuthManager
	UpdateControlManager *UpdateManager
	Mender               Controller
	Sctx                 StateContext
	Store                store.Store
	ForceToState         chan State
	stop                 bool
}

func NewDaemon(
	config *conf.MenderConfig,
	mender Controller,
	store store.Store,
	authManager AuthManager) (*MenderDaemon, error) {

	updmgr := NewUpdateManager(mender.GetControlMapPool(),
		config.UpdateControlMapExpirationTimeSeconds)
	if config.DBus.Enabled {
		api, err := dbus.GetDBusAPI()
		if err != nil {
			return nil, errors.Wrap(err, "DBus API support not available, but DBus is enabled")
		}
		updmgr.EnableDBus(api)
	}

	daemon := MenderDaemon{
		AuthManager:          authManager,
		UpdateControlManager: updmgr,
		Mender:               mender,
		Sctx: StateContext{
			Store:         store,
			Rebooter:      system.NewSystemRebootCmd(system.OsCalls{}),
			WakeupChan:    make(chan bool, 1),
			pauseReported: make(map[string]bool),
		},
		Store:        store,
		ForceToState: make(chan State, 1),
	}
	return &daemon, nil
}

func (d *MenderDaemon) StopDaemon() {
	d.stop = true
}

func (d *MenderDaemon) Cleanup() {
	if d.Store != nil {
		if err := d.Store.Close(); err != nil {
			log.Errorf("Failed to close data store: %v", err)
		}
		d.Store = nil
	}
}

func (d *MenderDaemon) shouldStop() bool {
	return d.stop
}

func (d *MenderDaemon) Run() error {
	// Start the auth Manager in a different go routine, if set
	if d.AuthManager != nil {
		d.AuthManager.Start()
		defer d.AuthManager.Stop()
	}
	if d.UpdateControlManager != nil {
		cancel, err := d.UpdateControlManager.Start()
		if err != nil {
			log.Error(err)
		} else {
			defer cancel()
		}
	}

	// set the first state transition
	var toState State = d.Mender.GetCurrentState()
	cancelled := false
	for {
		// If signal SIGUSR1 or SIGUSR2 is received, force the state-machine to the correct state.
		select {
		case nState := <-d.ForceToState:
			switch toState.(type) {
			case *idleState,
				*checkWaitState,
				*updateCheckState,
				*inventoryUpdateState:
				log.Infof("Forcing state machine to: %s", nState)
				toState = nState
			default:
				log.Errorf("Cannot check update or update inventory while in %s state", toState)
			}

		default:
			// Identity op - do nothing.
		}
		toState, cancelled = d.Mender.TransitionState(toState, &d.Sctx)
		if toState.Id() == datastore.MenderStateError {
			es, ok := toState.(*errorState)
			if ok {
				if es.IsFatal() {
					return es.cause
				}
			} else {
				return errors.New("failed")
			}
		}
		if cancelled || toState.Id() == datastore.MenderStateDone {
			break
		}
		if d.shouldStop() {
			log.Infof("Shutting down.")
			return nil
		}
	}
	return nil
}

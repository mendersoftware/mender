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
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mendersoftware/mender/dbus"
	"github.com/mendersoftware/mender/utils"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	updateManagerSetUpdateControlMap = "SetUpdateControlMap"
	UpdateManagerDBusPath            = "/io/mender/UpdateManager"
	UpdateManagerDBusObjectName      = "io.mender.UpdateManager"
	UpdateManagerDBusInterfaceName   = "io.mender.Update1"
	UpdateManagerDBusInterface       = `
	    <node>
	      <interface name="io.mender.Update1">
		<method name="SetUpdateControlMap">
		  <arg type="s" name="update_control_map" direction="in"/>
		  <arg type="i" name="refresh_timeout" direction="out"/>
		</method>
	      </interface>
	    </node>`
)

var UpdateManagerMapPool *ControlMapPool

func init() {
	UpdateManagerMapPool = NewControlMap()
}

type UpdateManager struct {
	dbus                        dbus.DBusAPI
	updateControlTimeoutSeconds int
}

func NewUpdateManager(updateControlTimeoutSeconds int) *UpdateManager {
	return &UpdateManager{
		updateControlTimeoutSeconds: updateControlTimeoutSeconds,
	}
}

func (u *UpdateManager) EnableDBus(api dbus.DBusAPI) {
	u.dbus = api
}

func (u *UpdateManager) Start() (context.CancelFunc, error) {
	log.Debug("Running the UpdateManager")
	if u.dbus == nil {
		return nil, errors.New("DBus not enabled")
	}
	ctx, cancel := context.WithCancel(context.Background())
	go u.run(ctx)
	return cancel, nil
}

func (u *UpdateManager) run(ctx context.Context) error {
	log.Debug("Running the UpdateManager")
	dbusConn, err := u.dbus.BusGet(dbus.GBusTypeSystem)
	if err != nil {
		return errors.Wrap(err, "Failed to start the Update manager")
	}
	nameGID, err := u.dbus.BusOwnNameOnConnection(
		dbusConn,
		UpdateManagerDBusObjectName,
		dbus.DBusNameOwnerFlagsAllowReplacement|dbus.DBusNameOwnerFlagsReplace,
	)
	if err != nil {
		return errors.Wrap(err, "Failed to start the update manager")
	}
	defer u.dbus.BusUnownName(nameGID)
	intGID, err := u.dbus.BusRegisterInterface(
		dbusConn,
		UpdateManagerDBusPath,
		UpdateManagerDBusInterface,
	)
	if err != nil {
		log.Errorf("Failed to register the DBus interface name %q at path %q: %s",
			UpdateManagerDBusInterface,
			UpdateManagerDBusPath,
			err,
		)
		return err
	}
	defer u.dbus.BusUnregisterInterface(dbusConn, intGID)

	u.dbus.RegisterMethodCallCallback(
		UpdateManagerDBusPath,
		UpdateManagerDBusInterfaceName,
		updateManagerSetUpdateControlMap,
		func(_ string, _ string, _ string, updateControlMap string) (interface{}, error) {
			// Unmarshal json disallowing unknown fields
			controlMap := UpdateControlMap{}
			decoder := json.NewDecoder(strings.NewReader(updateControlMap))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&controlMap); err != nil {
				log.Errorf("Failed to unmarshal the JSON in the string received via SetUpdateControlMap on D-Bus: %s", err)
				return u.updateControlTimeoutSeconds / 2, err
			}

			// Validate and sanitize the map
			if err := controlMap.Validate(); err != nil {
				log.Errorf("Failed to validate the UpdateControlMap: %s", err)
				return u.updateControlTimeoutSeconds / 2, err
			}
			controlMap.Sanitize()
			if len(controlMap.States) == 0 {
				log.Debugf("Ignoring UpdateControlMap %s with no non-default States", controlMap.ID)
			} else {
				log.Debugf("Setting the control map parameter to: %s", controlMap.ID)
				UpdateManagerMapPool.Insert(controlMap.Stamp(u.updateControlTimeoutSeconds))
			}

			return u.updateControlTimeoutSeconds / 2, nil
		})
	defer u.dbus.UnregisterMethodCallCallback(
		UpdateManagerDBusPath,
		UpdateManagerDBusInterfaceName,
		updateManagerSetUpdateControlMap)
	<-ctx.Done()
	return nil
}

type UpdateControlMapState struct {
	Action           string `json:"action"`
	OnMapExpire      string `json:"on_map_expire"`
	OnActionExecuted string `json:"on_action_executed"`
}

type UpdateControlMap struct {
	ID         string                           `json:"id"`
	Priority   int                              `json:"priority"`
	States     map[string]UpdateControlMapState `json:"states"`
	expiryTime time.Time
	expired    bool
}

func (u *UpdateControlMap) Stamp(updateControlTimeoutSeconds int) *UpdateControlMap {
	u.expiryTime = time.Now().Add(
		time.Duration(updateControlTimeoutSeconds) * time.Second)
	// expiration monitor
	go func() {
		<-time.After(time.Duration(updateControlTimeoutSeconds) * time.Second)
		u.expired = true
	}()
	return u
}

var UpdateControlMapStateKeyValid []string = []string{
	"ArtifactInstall_Enter",
	"ArtifactReboot_Enter",
	"ArtifactCommit_Enter",
}
var UpdateControlMapStateActionValid []string = []string{
	"continue",
	"force_continue",
	"pause",
	"fail",
}
var UpdateControlMapStateOnMapExpireValid []string = []string{
	"continue",
	"force_continue",
	"fail",
}
var UpdateControlMapStateOnActionExecutedValid []string = UpdateControlMapStateActionValid

var UpdateControlMapStateActionDefault string = "continue"

func UpdateControlMapStateOnMapExpireDefault(action string) string {
	if action == "pause" {
		return "fail"
	}
	return action
}

var UpdateControlMapStateOnActionExecutedDefault = UpdateControlMapStateActionDefault

func (s UpdateControlMapState) Validate() error {
	if s.Action != "" {
		found, err := utils.ElemInSlice(UpdateControlMapStateActionValid, s.Action)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("invalid value %q", s.Action)
		}
	}

	if s.OnMapExpire != "" {
		found, err := utils.ElemInSlice(UpdateControlMapStateOnMapExpireValid, s.OnMapExpire)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("invalid value %q", s.OnMapExpire)
		}
	}

	if s.OnActionExecuted != "" {
		found, err := utils.ElemInSlice(UpdateControlMapStateOnActionExecutedValid, s.OnActionExecuted)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("invalid value %q", s.OnActionExecuted)
		}
	}

	return nil
}

func (s *UpdateControlMapState) Sanitize() {
	if s.Action == "" {
		log.Debugf("Action was empty, setting to default %q", UpdateControlMapStateActionDefault)
		s.Action = UpdateControlMapStateActionDefault
	}
	if s.OnMapExpire == "" {
		onMapExpireDefault := UpdateControlMapStateOnMapExpireDefault(s.Action)
		log.Debugf("OnMapExpire was empty, setting to default %q", onMapExpireDefault)
		s.OnMapExpire = onMapExpireDefault
	}

	if s.OnActionExecuted == "" {
		log.Debugf("OnActionExecuted was empty, setting to default %q", UpdateControlMapStateOnActionExecutedDefault)
		s.OnActionExecuted = UpdateControlMapStateOnActionExecutedDefault
	}
}

func (m UpdateControlMap) Validate() error {
	// ID is mandatory
	if m.ID == "" {
		return errors.New("ID cannot be empty")
	}

	// Priority must be 0 or higher
	if m.Priority < 0 {
		return fmt.Errorf("invalid Priority %q, value must be 0 or higher", m.Priority)
	}

	// Check valid States keys
	for stateKey := range m.States {
		found, err := utils.ElemInSlice(UpdateControlMapStateKeyValid, stateKey)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("invalid value %q", stateKey)
		}
	}

	// Validate each State
	for _, state := range m.States {
		if err := state.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func (m UpdateControlMap) Sanitize() {
	defaultState := UpdateControlMapState{
		Action:           UpdateControlMapStateActionDefault,
		OnMapExpire:      UpdateControlMapStateOnMapExpireDefault(UpdateControlMapStateActionDefault),
		OnActionExecuted: UpdateControlMapStateOnActionExecutedDefault,
	}
	for stateKey, state := range m.States {
		state.Sanitize()
		if state == defaultState {
			log.Debugf("Default state %q, removing", stateKey)
			delete(m.States, stateKey)
		}
	}
}

func (u *UpdateControlMap) Expired() bool {
	return u.expired
}

func (u *UpdateControlMap) Equal(other *UpdateControlMap) bool {
	if u.ID != other.ID {
		return false
	}
	if u.Priority != other.Priority {
		return false
	}
	return true
}

func (u *UpdateControlMap) String() string {
	return fmt.Sprintf("ID: %s Priority: %d ", u.ID, u.Priority)
}

type ControlMapPool struct {
	Pool  []*UpdateControlMap
	mutex sync.Mutex
}

func NewControlMap() *ControlMapPool {
	return &ControlMapPool{
		Pool: []*UpdateControlMap{},
	}
}

func (c *ControlMapPool) Insert(cm *UpdateControlMap) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	nm := []*UpdateControlMap{}
	query(
		c.Pool,
		// Collector
		func(u *UpdateControlMap) {
			nm = append(nm, u)
		},
		// Predicates
		func(u *UpdateControlMap) bool { return !cm.Equal(u) },
	)
	c.Pool = append(nm, cm)
}

func (c *ControlMapPool) Get(ID string) (active []*UpdateControlMap, expired []*UpdateControlMap) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	query(
		c.Pool,
		// Collector
		func(u *UpdateControlMap) {
			if u.expired {
				expired = append(expired, u)
				return
			}
			active = append(active, u)
		},
		// Predicates
		func(u *UpdateControlMap) bool {
			return u.ID == ID
		},
	)
	return active, expired
}

func (c *ControlMapPool) ClearExpired() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	newPool := []*UpdateControlMap{}
	query(
		c.Pool,
		// Collector
		func(u *UpdateControlMap) {
			newPool = append(newPool, u)
		},
		// Predicates
		func(u *UpdateControlMap) bool {
			return !u.Expired()
		},
	)
	c.Pool = newPool
}

// query is a utility function to run a 'closure' on all values of
// the list 'm' matching the predicates in 'predicates...'
func query(m []*UpdateControlMap, f func(*UpdateControlMap), predicates ...func(*UpdateControlMap) bool) {
	for _, e := range m {
		// Sentinels
		for _, predicate := range predicates {
			if !predicate(e) {
				goto nextElement
			}
		}
		f(e)
	nextElement:
	}
}

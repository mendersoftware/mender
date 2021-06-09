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
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mendersoftware/mender/datastore"
	"github.com/mendersoftware/mender/dbus"
	"github.com/mendersoftware/mender/store"
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

type UpdateManager struct {
	dbus                        dbus.DBusAPI
	controlMapPool              *ControlMapPool
	updateControlTimeoutSeconds int
}

func NewUpdateManager(controlMapPool *ControlMapPool, updateControlTimeoutSeconds int) *UpdateManager {
	return &UpdateManager{
		controlMapPool:              controlMapPool,
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
				log.Debugf("Deleting UpdateControlMap %s with no non-default States", controlMap.ID)
				u.controlMapPool.Delete(controlMap.ID)
			} else {
				log.Debugf("Setting the control map parameter to: %s", controlMap.ID)
				controlMap.expirationChannel = u.controlMapPool.Updates
				u.controlMapPool.Insert(controlMap.Stamp(u.updateControlTimeoutSeconds))
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
	ID                string                           `json:"id"`
	Priority          int                              `json:"priority"`
	States            map[string]UpdateControlMapState `json:"states"`
	expiryTime        time.Time
	expired           bool
	mutex             sync.Mutex
	expirationChannel chan struct{}
}

func (u *UpdateControlMap) Stamp(updateControlTimeoutSeconds int) *UpdateControlMap {
	u.expiryTime = time.Now().Add(
		time.Duration(updateControlTimeoutSeconds) * time.Second)
	// expiration monitor
	u.expire(time.Duration(updateControlTimeoutSeconds) * time.Second)
	return u
}

func (u *UpdateControlMap) expire(in time.Duration) {
	go func() {
		<-time.After(in)
		u.mutex.Lock()
		defer u.mutex.Unlock()
		u.expired = true
		select {
		case u.expirationChannel <- struct{}{}:
			log.Debugf("ControlMapPool: Map %v has expired", u)
		default:
		}
	}()
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

func UpdateControlMapStateOnActionExecutedDefault(action string) string {
	return action
}

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
		s.OnActionExecuted = UpdateControlMapStateOnActionExecutedDefault(s.Action)
		log.Debugf("OnActionExecuted was empty, setting to default %q", s.OnActionExecuted)
	}
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range strings.ToLower(s) {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
	}

	return true
}

func (m UpdateControlMap) Validate() error {
	// ID must be a UUID.
	if !isUUID(m.ID) {
		return errors.New("ID must be a UUID")
	}

	// Priority must be in range [-10,10]
	if m.Priority < -10 || m.Priority > 10 {
		return fmt.Errorf("invalid Priority %q, value must be in the range [-10, 10]", m.Priority)
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
		OnActionExecuted: UpdateControlMapStateOnActionExecutedDefault(UpdateControlMapStateActionDefault),
	}
	for stateKey, state := range m.States {
		state.Sanitize()
		m.States[stateKey] = state
		if state == defaultState {
			log.Debugf("Default state %q, removing", stateKey)
			delete(m.States, stateKey)
		}
	}
}

func (u *UpdateControlMap) Expired() bool {
	u.mutex.Lock()
	defer u.mutex.Unlock()
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

func (u *UpdateControlMap) action(state string) string {
	_, ok := u.States[state]
	if !ok {
		log.Errorf("The state %q was not found in the control map. This should never happen", state)
		return ""
	}
	if u.Expired() {
		return u.States[state].OnMapExpire
	}
	return u.States[state].Action
}

func (u *UpdateControlMap) String() string {
	return fmt.Sprintf("ID: %s Priority: %d ", u.ID, u.Priority)
}

type ControlMapPool struct {
	Pool    []*UpdateControlMap
	mutex   sync.Mutex
	store   store.Store
	Updates chan struct{} // Announces all updates to the maps
}

// loadTimeout is how far in the future to set the map expiry when loading from
// the store.
func NewControlMap(store store.Store, loadTimeout int) *ControlMapPool {
	pool := &ControlMapPool{
		Pool:    []*UpdateControlMap{},
		store:   store,
		Updates: make(chan struct{}, 1),
	}

	pool.loadFromStore(loadTimeout)

	return pool
}

func (c *ControlMapPool) SetStore(store store.Store) {
	c.store = store
}

func (c *ControlMapPool) Insert(cm *UpdateControlMap) {
	log.Debugf("Inserting Update Control Map: %v", cm)
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
	c.saveToStore()
	c.announceUpdate()
}

func (c *ControlMapPool) announceUpdate() {
	select {
	case c.Updates <- struct{}{}:
		log.Debug("ControlMapPool: Announcing update to the map")
	default:
	}
}

func (c *ControlMapPool) Get(ID string) (active []*UpdateControlMap, expired []*UpdateControlMap) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	query(
		c.Pool,
		// Collector
		func(u *UpdateControlMap) {
			if u.Expired() {
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

func (c *ControlMapPool) Delete(ID string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	filteredPool := []*UpdateControlMap{}
	query(
		c.Pool,
		// Collector
		func(u *UpdateControlMap) {
			filteredPool = append(filteredPool, u)
		},
		// Predicates
		func(u *UpdateControlMap) bool {
			return u.ID != ID
		},
	)
	c.Pool = filteredPool
	c.saveToStore()
	c.announceUpdate()
}

func (c *ControlMapPool) ClearExpired() {
	log.Debug("Clearing expired Update Control Maps")
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

	c.saveToStore()
}

type controlMapPoolDBFormat struct {
	Active  []*UpdateControlMap `json:"active"`
	Expired []*UpdateControlMap `json:"expired"`
}

// Must only be called from functions that have already locked the mutex.
func (c *ControlMapPool) saveToStore() {
	if c.store == nil {
		// Saving not enabled. This should not happen outside tests.
		return
	}

	var active, expired []*UpdateControlMap
	query(
		c.Pool,
		func(m *UpdateControlMap) {
			if m.expired {
				expired = append(expired, m)
			} else {
				active = append(active, m)
			}
		},
	)

	toSave := controlMapPoolDBFormat{
		// Intentionally not using label assignment here. We want to
		// know if we're missing any fields.
		active,
		expired,
	}

	data, err := json.Marshal(&toSave)
	if err != nil {
		log.Errorf("Could not marshal Update Control Maps to JSON: %s", err.Error())
		// There isn't much we can do if it fails.
		return
	}

	log.Debugf("Saving Update Control Maps to disk: %q", string(data))

	err = c.store.WriteAll(datastore.UpdateControlMaps, data)
	if err != nil {
		log.Errorf("Could not save Update Control Maps to database: %s", err.Error())
		// There isn't much we can do if it fails.
		return
	}
}

// `timeout` is the timeout that should be given to expired maps that we load
// from disk.
func (c *ControlMapPool) loadFromStore(timeout int) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	data, err := c.store.ReadAll(datastore.UpdateControlMaps)
	if errors.Is(err, os.ErrNotExist) {
		log.Debug("No Update Control Maps found in database.")
		return
	} else if err != nil {
		log.Errorf("Could not read Update Control Maps from database: %s", err.Error())
		// There isn't much we can do if it fails.
		return
	}

	var maps controlMapPoolDBFormat
	err = json.Unmarshal(data, &maps)
	if err != nil {
		log.Errorf("Could not unmarshal Update Control Maps from JSON: %s", err.Error())
		// There isn't much we can do if it fails.
		return
	}

	for _, m := range maps.Active {
		m.expirationChannel = c.Updates
		m.Stamp(timeout)
	}
	c.Pool = maps.Active

	for _, m := range maps.Expired {
		m.expirationChannel = c.Updates
		m.Stamp(timeout)
		m.expired = true
	}
	c.Pool = append(c.Pool, maps.Expired...)
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

// QueryAndUpdate queries the map pool for the correct action to return
func (c *ControlMapPool) QueryAndUpdate(state string) (action string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	defer c.saveToStore()

	maps := c.Pool
	log.Debugf("Querying Update Control maps. Currently active maps: '%v'", maps)
	sort.Slice(maps, func(i, j int) bool {
		return maps[i].Priority > maps[j].Priority
	})
	actions := []string{}
	for _, priority := range uniquePriorities(maps) {
		query(maps,
			// Collector
			func(u *UpdateControlMap) {
				actions = append(actions, u.action(state))
				// Upgrade with the value from `on_action_executed`
				s := u.States[state]
				s.Action = u.States[state].OnActionExecuted
				u.States[state] = s
			},
			// Predicates
			func(u *UpdateControlMap) bool {
				return u.Priority == priority
			},
			func(u *UpdateControlMap) bool {
				_, hasState := u.States[state]
				return hasState
			},
		)
		v := queryActionList(actions)
		if v != "" {
			log.Debugf("Returning action %q", v)
			return v
		}
	}
	// No valid values
	log.Debug("Returning action \"continue\"")
	return "continue"
}

// uniquePriorities returns a list of the unique elements in the list 'm', and
// 'm' must be sorted.
func uniquePriorities(m []*UpdateControlMap) []int {
	ps := []int{}
	if len(m) == 0 {
		return ps
	}
	oldPri := m[0].Priority
	ps = append(ps, m[0].Priority)
	for _, e := range m {
		if e.Priority != oldPri {
			ps = append(ps, e.Priority)
			oldPri = e.Priority
		}
	}
	return ps
}

// queryActionList returns the next action from the given list,
// according to this algorithm:
//
// 1. If "fail" exists in the list, return "fail".
// 2. If "pause" exists in the list, return "pause".
// 2. If "force_continue" exists in the list, return "continue" (<-- note the difference here)
// 4. "continue" should be ignored, don't return it
//
func queryActionList(stateActions []string) string {
	// 1. If fail exists in the list, return fail
	pauseExists := false
	forceContinueExists := false
	for _, e := range stateActions {
		if e == "fail" {
			return "fail"
		}
		if e == "pause" {
			pauseExists = true
		}
		if e == "force_continue" {
			forceContinueExists = true
		}
	}
	if pauseExists {
		return "pause"
	}
	if forceContinueExists {
		return "continue"
	}
	return ""
}

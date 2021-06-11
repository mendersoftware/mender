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
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/mendersoftware/mender/app/updatecontrolmap"
	"github.com/mendersoftware/mender/datastore"
	"github.com/mendersoftware/mender/dbus"
	"github.com/mendersoftware/mender/store"

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
			controlMap := updatecontrolmap.UpdateControlMap{}
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
				controlMap.ExpirationChannel = u.controlMapPool.Updates
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

type ControlMapPool struct {
	Pool    []*updatecontrolmap.UpdateControlMap
	mutex   sync.Mutex
	store   store.Store
	Updates chan struct{} // Announces all updates to the maps
}

// loadTimeout is how far in the future to set the map expiry when loading from
// the store.
func NewControlMap(store store.Store, loadTimeout int) *ControlMapPool {
	pool := &ControlMapPool{
		Pool:    []*updatecontrolmap.UpdateControlMap{},
		store:   store,
		Updates: make(chan struct{}, 1),
	}

	pool.loadFromStore(loadTimeout)

	return pool
}

func (c *ControlMapPool) SetStore(store store.Store) {
	c.store = store
}

func (c *ControlMapPool) Insert(cm *updatecontrolmap.UpdateControlMap) {
	log.Debugf("Inserting Update Control Map: %v", cm)
	c.mutex.Lock()
	defer c.mutex.Unlock()
	nm := []*updatecontrolmap.UpdateControlMap{}
	query(
		c.Pool,
		// Collector
		func(u *updatecontrolmap.UpdateControlMap) {
			nm = append(nm, u)
		},
		// Predicates
		func(u *updatecontrolmap.UpdateControlMap) bool { return !cm.Equal(u) },
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

func (c *ControlMapPool) Get(ID string) (active []*updatecontrolmap.UpdateControlMap, expired []*updatecontrolmap.UpdateControlMap) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	query(
		c.Pool,
		// Collector
		func(u *updatecontrolmap.UpdateControlMap) {
			if u.Expired() {
				expired = append(expired, u)
				return
			}
			active = append(active, u)
		},
		// Predicates
		func(u *updatecontrolmap.UpdateControlMap) bool {
			return u.ID == ID
		},
	)
	return active, expired
}

func (c *ControlMapPool) Delete(ID string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	filteredPool := []*updatecontrolmap.UpdateControlMap{}
	query(
		c.Pool,
		// Collector
		func(u *updatecontrolmap.UpdateControlMap) {
			filteredPool = append(filteredPool, u)
		},
		// Predicates
		func(u *updatecontrolmap.UpdateControlMap) bool {
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
	filteredPool := []*updatecontrolmap.UpdateControlMap{}
	query(
		c.Pool,
		// Collector
		func(u *updatecontrolmap.UpdateControlMap) {
			filteredPool = append(filteredPool, u)
		},
		// Predicates
		func(u *updatecontrolmap.UpdateControlMap) bool {
			return !u.Expired()
		},
	)
	c.Pool = filteredPool

	c.saveToStore()
}

type controlMapPoolDBFormat struct {
	Active  []*updatecontrolmap.UpdateControlMap `json:"active"`
	Expired []*updatecontrolmap.UpdateControlMap `json:"expired"`
}

// Must only be called from functions that have already locked the mutex.
func (c *ControlMapPool) saveToStore() {
	if c.store == nil {
		// Saving not enabled. This should not happen outside tests.
		return
	}

	var active, expired []*updatecontrolmap.UpdateControlMap
	query(
		c.Pool,
		func(m *updatecontrolmap.UpdateControlMap) {
			if m.Expired() {
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
		m.ExpirationChannel = c.Updates
		m.Stamp(timeout)
	}
	c.Pool = maps.Active

	for _, m := range maps.Expired {
		m.ExpirationChannel = c.Updates
		m.Stamp(timeout)
		m.Expire()
	}
	c.Pool = append(c.Pool, maps.Expired...)
}

// query is a utility function to run a 'closure' on all values of
// the list 'm' matching the predicates in 'predicates...'
func query(m []*updatecontrolmap.UpdateControlMap, f func(*updatecontrolmap.UpdateControlMap), predicates ...func(*updatecontrolmap.UpdateControlMap) bool) {
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
			func(u *updatecontrolmap.UpdateControlMap) {
				actions = append(actions, u.Action(state))
				// Upgrade with the value from `on_action_executed`
				s := u.States[state]
				s.Action = u.States[state].OnActionExecuted
				u.States[state] = s
			},
			// Predicates
			func(u *updatecontrolmap.UpdateControlMap) bool {
				return u.Priority == priority
			},
			func(u *updatecontrolmap.UpdateControlMap) bool {
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
func uniquePriorities(m []*updatecontrolmap.UpdateControlMap) []int {
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

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

package updatecontrolmap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/mendersoftware/mender/utils"

	log "github.com/sirupsen/logrus"
)

type UpdateControlMap struct {
	ID                string                           `json:"id"`
	Priority          int                              `json:"priority"`
	States            map[string]UpdateControlMapState `json:"states"`
	Stamped           time.Time                        `json:"-"`
	HalfWayTime       time.Time                        `json:"-"`
	ExpiryTime        time.Time                        `json:"-"`
	expired           bool
	mutex             sync.Mutex
	ExpirationChannel chan bool `json:"-"`
}

func (u *UpdateControlMap) Stamp(updateControlTimeoutSeconds int) *UpdateControlMap {
	u.Stamped = time.Now()
	u.ExpiryTime = u.Stamped.Add(
		time.Duration(updateControlTimeoutSeconds) * time.Second)
	u.HalfWayTime = u.Stamped.Add(
		time.Duration(updateControlTimeoutSeconds) * time.Second / 2)
	// expiration monitor
	u.expire(time.Duration(updateControlTimeoutSeconds) * time.Second)
	return u
}

func (u *UpdateControlMap) HalfwayTime() time.Time {
	return u.HalfWayTime
}

func (u *UpdateControlMap) expire(in time.Duration) {
	go func() {
		<-time.After(in)
		u.mutex.Lock()
		defer u.mutex.Unlock()
		u.expired = true
		select {
		case u.ExpirationChannel <- true:
			log.Debugf("ControlMapPool: Map %v has expired", u)
		default:
		}
	}()
}

type UpdateControlMapState struct {
	Action           string `json:"action"`
	OnMapExpire      string `json:"on_map_expire"`
	OnActionExecuted string `json:"on_action_executed"`
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

func (s *UpdateControlMapState) Validate() error {
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
		found, err := utils.ElemInSlice(
			UpdateControlMapStateOnActionExecutedValid,
			s.OnActionExecuted,
		)
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

func (m *UpdateControlMap) Validate() error {
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

func (m *UpdateControlMap) Sanitize() {
	defaultState := UpdateControlMapState{
		Action: UpdateControlMapStateActionDefault,
		OnMapExpire: UpdateControlMapStateOnMapExpireDefault(
			UpdateControlMapStateActionDefault,
		),
		OnActionExecuted: UpdateControlMapStateOnActionExecutedDefault(
			UpdateControlMapStateActionDefault,
		),
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

func (u *UpdateControlMap) Expire() {
	u.expired = true
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

func (u *UpdateControlMap) Action(state string) string {
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
	if u == nil {
		return ""
	}
	return fmt.Sprintf("ID: %s Priority: %d\nStates: %v", u.ID, u.Priority, u.States)
}

func (u *UpdateControlMap) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	if u == nil {
		return errors.New("Cannot unmarshal into a nil pointer")
	}

	type UpdateControlMapData UpdateControlMap

	updData := &UpdateControlMapData{}

	dec := json.NewDecoder(bytes.NewBuffer(data))
	dec.DisallowUnknownFields()
	err := dec.Decode(updData)
	if err != nil {
		return errors.Wrap(err, "Update Control Map contains unsupported fields")
	}

	*u = UpdateControlMap{
		updData.ID,
		updData.Priority,
		updData.States,
		updData.Stamped,
		updData.HalfWayTime,
		updData.ExpiryTime,
		updData.expired,
		// Copying a mutex triggers go vet warning, so avoid that.
		sync.Mutex{},
		updData.ExpirationChannel,
	}

	err = u.Validate()
	if err != nil {
		return err
	}

	u.Sanitize()

	return nil
}

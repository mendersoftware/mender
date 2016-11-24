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
	"sort"
	"time"

	"github.com/mendersoftware/log"
	"github.com/pkg/errors"
)

type Scheduler interface {
	ScheduleState(state State, freq time.Duration, once bool) error
	RemoveState(state State, freq time.Duration) error
	Wait(cancel chan struct{}) (State, bool)
}

type stateScheduler struct {
	events []*stateEvent
}

type byTime []*stateEvent

func (e byTime) Len() int      { return len(e) }
func (e byTime) Swap(i, j int) { e[i], e[j] = e[j], e[i] }
func (e byTime) Less(i, j int) bool {
	return e[i].runNext.Before(e[j].runNext)
}

type stateEvent struct {
	every   time.Duration
	state   State
	runOnce bool

	runNext time.Time
	id      string
}

func NewStateScheduler() Scheduler {
	return &stateScheduler{}
}

func getKey(t time.Duration, s State) string {
	return string(t) + s.Id().String()
}

func (s *stateScheduler) ScheduleState(state State, freq time.Duration, once bool) error {
	key := getKey(freq, state)

	for _, e := range s.events {
		if e.id == key {
			return errors.Errorf("State %s with frequency %s already scheduled",
				state.Id(), freq)
		}
	}

	log.Debugf("Scheduling state %s with frequency %s", state.Id(), freq)

	ev := &stateEvent{
		every:   freq,
		state:   state,
		runOnce: once,

		// calculate the next run time
		runNext: time.Now().Add(freq),
		id:      key,
	}
	s.events = append(s.events, ev)

	return nil
}

func (s *stateScheduler) RemoveState(state State, freq time.Duration) error {
	key := getKey(freq, state)

	for i, e := range s.events {
		if e.id == key {
			log.Debugf("Removing schedule for state %s with frequency %s",
				state.Id(), freq)

			// we are having a slice of pointers; make sure to garbage collect
			copy(s.events[:i], s.events[i+1:])
			s.events[len(s.events)-1] = nil
			s.events = s.events[:len(s.events)-1]
			return nil
		}
	}

	return errors.Errorf("State %s with frequency %s does not exist in scheduler",
		state.Id(), freq)
}

func (s *stateScheduler) checkCancelled(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func (s *stateScheduler) fire(event *stateEvent) State {
	log.Debugf("firing scheduled state event: %v", event)

	if event.runOnce {
		s.RemoveState(event.state, event.every)
	} else {
		event.runNext = time.Now().Add(event.every)
	}
	return event.state
}

func (s *stateScheduler) Wait(cancel chan struct{}) (State, bool) {
	if s.events == nil || len(s.events) == 0 {
		err := errors.New("no scheduled events to wait for")
		return NewErrorState(NewTransientError(err)), false
	}

	sort.Sort(byTime(s.events))
	log.Debugf("wait for events: %+v", s.events)

	now := time.Now()

	// if the event was missed run it immediately
	if s.events[0].runNext.Before(now) {
		log.Debugf("running immediately")
		return s.fire(s.events[0]), false
	}

	timer := time.NewTimer(s.events[0].runNext.Sub(now))

	select {
	case <-timer.C:
		log.Debugf("waiting for state '%s' complete", s.events[0].state.Id())

		if s.checkCancelled(cancel) {
			// TODO:
			err := errors.New("waiting canceled")
			return NewErrorState(NewTransientError(err)), true
		}

		return s.fire(s.events[0]), false

	case <-cancel:
		log.Info("shutting down...")
		timer.Stop()
	}

	// TODO:
	err := errors.New("waiting canceled")
	return NewErrorState(NewTransientError(err)), true
}

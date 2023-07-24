// Copyright 2023 Northern.tech AS
//
//	Licensed under the Apache License, Version 2.0 (the "License");
//	you may not use this file except in compliance with the License.
//	You may obtain a copy of the License at
//
//	    http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.
package app

import (
	"github.com/pkg/errors"
)

// mender specific error
type menderError interface {
	// cause of the error
	Cause() error
	// true if error is fatal
	IsFatal() bool
	// implements error interface
	error
}

type MenderError struct {
	cause error
	fatal bool
}

func (m *MenderError) Cause() error {
	return m.cause
}

func (m *MenderError) Unwrap() error {
	return m.cause
}

func (m *MenderError) IsFatal() bool {
	return m.fatal
}

func (m *MenderError) Error() string {
	var err error
	if m.fatal {
		err = errors.Wrapf(m.cause, "fatal error")
	} else {
		err = errors.Wrapf(m.cause, "transient error")
	}
	return err.Error()
}

// Create a new fatal error.
// Fatal errors will be reported back to the server.
func NewFatalError(err error) menderError {
	return &MenderError{
		cause: err,
		fatal: true,
	}
}

// Create a new transient error.
// Transient errors will normally not be reported back to the server, unless
// they persist long enough for the client to give up.
func NewTransientError(err error) menderError {
	return &MenderError{
		cause: err,
		fatal: false,
	}
}

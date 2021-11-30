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
//go:build !nodbus && cgo
// +build !nodbus,cgo

package dbus

import (
	"testing"

	"github.com/pkg/errors"

	"github.com/stretchr/testify/assert"

	"github.com/mendersoftware/mender/dbus/test"
)

func TestError(t *testing.T) {
	const errMessage = "error-message"
	gerror := Handle(test.ErrorToNative(errors.New(errMessage)))

	err := ErrorFromNative(gerror)
	assert.NotNil(t, err)
	assert.Error(t, err)

	assert.Equal(t, errMessage, err.Error())

	err = &Error{}
	assert.Equal(t, "", err.Error())
}

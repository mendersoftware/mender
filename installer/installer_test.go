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

package installer

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"testing"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/awriter"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func TestInstall(t *testing.T) {
	art, err := MakeRootfsImageArtifact(1, false)
	assert.NoError(t, err)
	assert.NotNil(t, art)

	// image not compatible with device
	err = Install(art, "fake-device", nil, nil)
	assert.Error(t, err)
	assert.Contains(t, errors.Cause(err).Error(),
		"image not compatible with device")

	art, err = MakeRootfsImageArtifact(1, false)
	assert.NoError(t, err)
	err = Install(art, "vexpress-qemu", nil, new(fDevice))
	assert.NoError(t, err)
}

func TestInstallSigned(t *testing.T) {
	art, err := MakeRootfsImageArtifact(2, true)
	assert.NoError(t, err)
	assert.NotNil(t, art)

	// no key for verifying artifact
	art, err = MakeRootfsImageArtifact(2, true)
	assert.NoError(t, err)
	err = Install(art, "vexpress-qemu", nil, new(fDevice))
	assert.Error(t, err)
	assert.Contains(t, errors.Cause(err).Error(),
		"failed to parse public key")

	// image not compatible with device
	art, err = MakeRootfsImageArtifact(2, true)
	assert.NoError(t, err)
	err = Install(art, "fake-device", []byte(PublicRSAKey), new(fDevice))
	assert.Error(t, err)
	assert.Contains(t, errors.Cause(err).Error(),
		"image not compatible with device")

	// installation successful
	art, err = MakeRootfsImageArtifact(2, true)
	assert.NoError(t, err)
	err = Install(art, "vexpress-qemu", []byte(PublicRSAKey), new(fDevice))
	assert.NoError(t, err)
}

type fDevice struct{}

func (d *fDevice) InstallUpdate(io.ReadCloser, int64) error { return nil }
func (d *fDevice) EnableUpdatedPartition() error            { return nil }

const (
	PublicRSAKey = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDSTLzZ9hQq3yBB+dMDVbKem6ia
v1J6opg6DICKkQ4M/yhlw32BCGm2ArM3VwQRgq6Q1sNSq953n5c1EO3Xcy/qTAKc
XwaUNml5EhW79AdibBXZiZt8fMhCjUd/4ce3rLNjnbIn1o9L6pzV4CcVJ8+iNhne
5vbA+63vRCnrc8QuYwIDAQAB
-----END PUBLIC KEY-----`
	PrivateRSAKey = `-----BEGIN RSA PRIVATE KEY-----
MIICXAIBAAKBgQDSTLzZ9hQq3yBB+dMDVbKem6iav1J6opg6DICKkQ4M/yhlw32B
CGm2ArM3VwQRgq6Q1sNSq953n5c1EO3Xcy/qTAKcXwaUNml5EhW79AdibBXZiZt8
fMhCjUd/4ce3rLNjnbIn1o9L6pzV4CcVJ8+iNhne5vbA+63vRCnrc8QuYwIDAQAB
AoGAQKIRELQOsrZsxZowfj/ia9jPUvAmO0apnn2lK/E07k2lbtFMS1H4m1XtGr8F
oxQU7rLyyP/FmeJUqJyRXLwsJzma13OpxkQtZmRpL9jEwevnunHYJfceVapQOJ7/
6Oz0pPWEq39GCn+tTMtgSmkEaSH8Ki9t32g9KuQIKBB2hbECQQDsg7D5fHQB1BXG
HJm9JmYYX0Yk6Z2SWBr4mLO0C4hHBnV5qPCLyevInmaCV2cOjDZ5Sz6iF5RK5mw7
qzvFa8ePAkEA46Anom3cNXO5pjfDmn2CoqUvMeyrJUFL5aU6W1S6iFprZ/YwdHcC
kS5yTngwVOmcnT65Vnycygn+tZan2A0h7QJBAJNlowZovDdjgEpeCqXp51irD6Dz
gsLwa6agK+Y6Ba0V5mJyma7UoT//D62NYOmdElnXPepwvXdMUQmCtpZbjBsCQD5H
VHDJlCV/yzyiJz9+tZ5giaAkO9NOoUBsy6GvdfXWn2prXmiPI0GrrpSvp7Gj1Tjk
r3rtT0ysHWd7l+Kx/SUCQGlitd5RDfdHl+gKrCwhNnRG7FzRLv5YOQV81+kh7SkU
73TXPIqLESVrqWKDfLwfsfEpV248MSRou+y0O1mtFpo=
-----END RSA PRIVATE KEY-----`
)

func MakeRootfsImageArtifact(version int, signed bool) (io.ReadCloser, error) {
	upd, err := MakeFakeUpdate("test update")
	if err != nil {
		return nil, err
	}
	defer os.Remove(upd)

	art := bytes.NewBuffer(nil)
	var aw *awriter.Writer
	if !signed {
		aw = awriter.NewWriter(art)
	} else {
		s := artifact.NewSigner([]byte(PrivateRSAKey))
		aw = awriter.NewWriterSigned(art, s)
	}
	var u handlers.Composer
	switch version {
	case 1:
		u = handlers.NewRootfsV1(upd)
	case 2:
		u = handlers.NewRootfsV2(upd)
	}

	updates := &awriter.Updates{U: []handlers.Composer{u}}
	err = aw.WriteArtifact("mender", version, []string{"vexpress-qemu"},
		"mender-1.1", updates)
	if err != nil {
		return nil, err
	}
	return &rc{art}, nil
}

func MakeFakeUpdate(data string) (string, error) {
	f, err := ioutil.TempFile("", "test_update")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if len(data) > 0 {
		if _, err := f.WriteString(data); err != nil {
			return "", err
		}
	}
	return f.Name(), nil
}

// io.ReadCloser interface
type rc struct {
	*bytes.Buffer
}

func (r *rc) Close() error {
	return nil
}

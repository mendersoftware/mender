// Copyright 2022 Northern.tech AS
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

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/awriter"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/mendersoftware/mender/conf"
)

func TestInstall(t *testing.T) {
	noUpdateProducers := AllModules{}
	updateProducers := AllModules{
		DualRootfs: new(fDevice),
	}

	art, err := MakeRootfsImageArtifact(2, false, false)
	assert.NoError(t, err)
	assert.NotNil(t, art)

	// image not compatible with device
	_, err = Install(art, "fake-device", nil, "", &noUpdateProducers)
	assert.Error(t, err)
	assert.Contains(t, errors.Cause(err).Error(),
		"not compatible with device fake-device")

	art, err = MakeRootfsImageArtifact(2, false, false)
	assert.NoError(t, err)
	_, err = Install(art, "vexpress-qemu", nil, "", &updateProducers)
	assert.NoError(t, err)
}

func TestInstallSigned(t *testing.T) {
	updateProducers := AllModules{
		DualRootfs: new(fDevice),
	}

	art, err := MakeRootfsImageArtifact(2, true, false)
	assert.NoError(t, err)
	assert.NotNil(t, art)

	// no key for verifying artifact
	art, err = MakeRootfsImageArtifact(2, true, false)
	assert.NoError(t, err)
	_, err = Install(art, "vexpress-qemu", nil, "", &updateProducers)
	assert.NoError(t, err)

	// image not compatible with device
	art, err = MakeRootfsImageArtifact(2, true, false)
	assert.NoError(t, err)
	keySelector := newFakeVerificationKeySelector()
	_, err = Install(art, "fake-device", keySelector, "", &updateProducers)
	assert.Error(t, err)
	assert.Contains(t, errors.Cause(err).Error(),
		"not compatible with device fake-device")

	// installation successful
	art, err = MakeRootfsImageArtifact(2, true, false)
	assert.NoError(t, err)
	_, err = Install(art, "vexpress-qemu", keySelector, "", &updateProducers)
	assert.NoError(t, err)

}

func TestInstallSignedRootfsKey(t *testing.T) {
	updateProducers := AllModules{
		DualRootfs: new(fDevice),
	}
	art, err := MakeRootfsImageArtifact(2, true, false)
	assert.NoError(t, err)
	assert.NotNil(t, art)

	// installation successful with a key selector that only has a signing key for rootfs-image update types.
	art, err = MakeRootfsImageArtifact(2, true, false)
	assert.NoError(t, err)
	_, err = Install(art, "vexpress-qemu", newRootfsOnlyVerificationKeySelector(), "", &updateProducers)
	assert.NoError(t, err)
}

func TestInstallNoSignature(t *testing.T) {
	updateProducers := AllModules{
		DualRootfs: new(fDevice),
	}

	art, err := MakeRootfsImageArtifact(2, false, false)
	assert.NoError(t, err)
	assert.NotNil(t, art)

	// image does not contain signature
	_, err = Install(art, "vexpress-qemu", newFakeVerificationKeySelector(), "", &updateProducers)
	assert.Error(t, err)
	assert.Contains(t, errors.Cause(err).Error(),
		"expecting signed artifact, but no signature file found")
}

func TestInstallWithScripts(t *testing.T) {
	updateProducers := AllModules{
		DualRootfs: new(fDevice),
	}

	art, err := MakeRootfsImageArtifact(2, false, true)
	assert.NoError(t, err)
	assert.NotNil(t, art)

	scrDir, err := ioutil.TempDir("", "test_scripts")
	assert.NoError(t, err)
	defer os.RemoveAll(scrDir)

	_, err = Install(art, "vexpress-qemu", nil, scrDir, &updateProducers)
	assert.NoError(t, err)
}

func TestCorrectUpdateProducerReturned(t *testing.T) {
	updateProducers := AllModules{
		DualRootfs: new(fDevice),
	}

	art, err := MakeRootfsImageArtifact(2, false, false)
	assert.NoError(t, err)
	assert.NotNil(t, art)

	returned, err := Install(art, "vexpress-qemu", nil, "", &updateProducers)
	assert.NoError(t, err)

	assert.Equal(t, 1, len(returned))
	assert.Equal(t, updateProducers.DualRootfs, returned[0])
}

func TestMultiplePayloadsRejected(t *testing.T) {
	updateProducers := AllModules{
		DualRootfs: new(fDevice),
	}

	art, err := MakeDoubleRootfsImageArtifact(3)
	require.NoError(t, err)

	_, err = Install(art, "vexpress-qemu", nil, "", &updateProducers)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Artifacts with more than one payload are not supported yet")
}

type fDevice struct{}

func (d *fDevice) Initialize(artifactHeaders,
	artifactAugmentedHeaders artifact.HeaderInfoer,
	payloadHeaders handlers.ArtifactUpdateHeaders) error {

	return nil
}

func (d *fDevice) PrepareStoreUpdate() error {
	return nil
}

func (d *fDevice) StoreUpdate(r io.Reader, info os.FileInfo) error {
	_, err := io.Copy(ioutil.Discard, r)
	return err
}

func (d *fDevice) FinishStoreUpdate() error {
	return nil
}

func (d *fDevice) InstallUpdate() error { return nil }

func (d *fDevice) Reboot() error {
	return nil
}

func (d *fDevice) CommitUpdate() error {
	return nil
}

func (d *fDevice) NeedsReboot() (RebootAction, error) {
	return RebootRequired, nil
}

func (d *fDevice) SupportsRollback() (bool, error) {
	return true, nil
}

func (d *fDevice) Rollback() error {
	return nil
}

func (d *fDevice) VerifyReboot() error {
	return nil
}

func (d *fDevice) RollbackReboot() error {
	return nil
}

func (d *fDevice) VerifyRollbackReboot() error {
	return nil
}

func (d *fDevice) Failure() error {
	return nil
}

func (d *fDevice) Cleanup() error {
	return nil
}

func (d *fDevice) GetType() string {
	return "vexpress-qemu"
}

func (d *fDevice) NewUpdateStorer(updateType *string, payload int) (handlers.UpdateStorer, error) {
	return d, nil
}

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
	PublicRSAKey2 = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQCSIyq+OiI+PdugpM4tLJNcG88X
Z8kNHPEcuWR6Xth/5WU7SloLtEYJ8cmWhDjEXObCyH3+zVdQ1umgRJDS5T0lwoRg
T3aotZ9bJ2XJQ2af8/FZCuLxEOKp1JtBNudaia2T3r/UV74cC8DZD7FCjFsl2qQW
AzULD4bwA94/hWW22wIDAQAB
-----END PUBLIC KEY-----`
	PrivateRSAKey2 = `-----BEGIN RSA PRIVATE KEY-----
MIICXQIBAAKBgQCSIyq+OiI+PdugpM4tLJNcG88XZ8kNHPEcuWR6Xth/5WU7SloL
tEYJ8cmWhDjEXObCyH3+zVdQ1umgRJDS5T0lwoRgT3aotZ9bJ2XJQ2af8/FZCuLx
EOKp1JtBNudaia2T3r/UV74cC8DZD7FCjFsl2qQWAzULD4bwA94/hWW22wIDAQAB
AoGAZMhF+QzEguJMLhyaaAMe2V4AUybrO9Ti36lnhxEUBBgi2WHsebfouYD7QoeL
UrizGFAGvIvGlOSyGCpRKnCX2v2sbK9jfXik0EiJ7+HrP4nRtgxrdaBqCiBYlRhA
nUQAPgR6PXAFNhj1qRXx2NFZWHpRLVutf16phRWcuifzR+kCQQDh3ebFLANQ9Vhf
5CnK3LvyZubGL4wMWx71xiDdFhH6gamYn27DFCZDnlboUsv2kXtwfJ+jleag1mb8
UPoK9L2tAkEApaI93UfnyukjOKPCDn6n+kLTHL5NqlSnN4/rhGKDdOjxWwrm7KnK
HFPGQWJySPKfq/qArticJF8kyR+PKg9HpwJBAMtMvYOp+w4q17HwH8Ht7unfv0aR
03/noLVd8YSucd5GSU4L61mB0HM6mUUiCV5VUoNMWTCYI2+PrEDd7kJgSj0CQDSb
PgjdAKq6t1wS7tyJr7JVrRWQ/7vcnSuRg10NqPDl11pyMPvzxWSP2wUDTocKwFnv
+xUNaTJIIbfbVS4nojsCQQDNFWkmt2iAbDpGWl3Mh5o7FnySSPhECVUIc7sBNANk
sDEUPmpTrTVQBL0Dv+TqyXghLOanV7KB/Ogq+EzV46Rb
-----END RSA PRIVATE KEY-----`
)

// newRootfsOnlyVerificationKeySelector returns 2 keys, both only allowed
// for rootfs-image types.
func newRootfsOnlyVerificationKeySelector() *fakeVerificationKeySelector {
	return &fakeVerificationKeySelector{
		keys: []*conf.VerificationKey{
			{
				Data: []byte(PublicRSAKey),
				Config: &conf.VerificationKeyConfig{
					Path:        "/etc/my/vkey.pub",
					UpdateTypes: []string{"rootfs-image"},
				},
			},
			{
				Data: []byte(PublicRSAKey2),
				Config: &conf.VerificationKeyConfig{
					Path:        "/etc/my/vkey2.pub",
					UpdateTypes: []string{"rootfs-image"},
				},
			},
		},
	}
}

// newFakeVerificationKeySelector returns a rootfs-image verification key,
// which can't be used to verify the data.
// As well as a default verification key that successfully signs the image.
func newFakeVerificationKeySelector() *fakeVerificationKeySelector {
	return &fakeVerificationKeySelector{
		keys: []*conf.VerificationKey{
			{
				Data: []byte(PublicRSAKey2),
				Config: &conf.VerificationKeyConfig{
					Path:        "/etc/my/vkey2.pub",
					UpdateTypes: []string{"rootfs-image"},
				},
			},
			{
				Data: []byte(PublicRSAKey),
				Config: &conf.VerificationKeyConfig{
					Path: "/etc/my/vkey.pub",
				},
			},
		},
	}
}

type fakeVerificationKeySelector struct {
	keys []*conf.VerificationKey
}

func (f *fakeVerificationKeySelector) SelectVerificationKeys(updateTypes ...string) []*conf.VerificationKey {
	var out []*conf.VerificationKey
	for _, key := range f.keys {
		if len(key.Config.UpdateTypes) == 0 || checkAnyMatch(key.Config.UpdateTypes, updateTypes) {
			out = append(out, key)
		}
	}
	return out
}

func checkAnyMatch(a, b []string) bool {
	for _, aItem := range a {
		for _, bItem := range b {
			if aItem == bItem {
				return true
			}
		}
	}
	return false
}

func MakeRootfsImageArtifact(version int, signed bool,
	hasScripts bool) (io.ReadCloser, error) {
	upd, err := MakeFakeUpdate("test update")
	if err != nil {
		return nil, err
	}
	defer os.Remove(upd)

	art := bytes.NewBuffer(nil)
	var aw *awriter.Writer
	comp := artifact.NewCompressorGzip()
	if !signed {
		aw = awriter.NewWriter(art, comp)
	} else {
		s, err := artifact.NewPKISigner([]byte(PrivateRSAKey))
		if err != nil {
			return nil, err
		}
		aw = awriter.NewWriterSigned(art, comp, s)
	}
	var u handlers.Composer
	switch version {
	case 1:
		return nil, errors.New("Artifact version 1 is deprecated")
	case 2:
		u = handlers.NewRootfsV2(upd)
	}

	scr := artifact.Scripts{}
	if hasScripts {
		s, ferr := ioutil.TempFile("", "ArtifactInstall_Enter_10_")
		if ferr != nil {
			return nil, err
		}
		defer os.Remove(s.Name())

		_, err = io.WriteString(s, "execute me!")
		if err != nil {
			return nil, err
		}

		if err = scr.Add(s.Name()); err != nil {
			return nil, err
		}
	}

	updates := &awriter.Updates{Updates: []handlers.Composer{u}}
	err = aw.WriteArtifact(&awriter.WriteArtifactArgs{
		Format:  "mender",
		Version: version,
		Devices: []string{"vexpress-qemu"},
		Name:    "mender-1.1",
		Updates: updates,
		Scripts: &scr,
	})
	if err != nil {
		return nil, err
	}
	return &rc{art}, nil
}

func MakeDoubleRootfsImageArtifact(version int) (io.ReadCloser, error) {
	upd, err := MakeFakeUpdate("test update")
	if err != nil {
		return nil, err
	}
	defer os.Remove(upd)

	art := bytes.NewBuffer(nil)
	aw := awriter.NewWriter(art, artifact.NewCompressorGzip())
	u := handlers.NewRootfsV3(upd)
	u2 := handlers.NewRootfsV3(upd)

	scr := artifact.Scripts{}

	depends := artifact.ArtifactDepends{
		CompatibleDevices: []string{"vexpress-qemu"},
	}
	provides := artifact.ArtifactProvides{
		ArtifactName: "artifact-name",
	}
	updateType := "rootfs-image"
	typeInfoV3 := artifact.TypeInfoV3{
		Type: &updateType,
	}

	updates := &awriter.Updates{Updates: []handlers.Composer{u, u2}}
	err = aw.WriteArtifact(&awriter.WriteArtifactArgs{
		Format:     "mender",
		Version:    version,
		Devices:    []string{"vexpress-qemu"},
		Name:       "mender-1.1",
		Updates:    updates,
		Scripts:    &scr,
		Depends:    &depends,
		Provides:   &provides,
		TypeInfoV3: &typeInfoV3,
	})
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

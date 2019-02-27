// Copyright 2019 Northern.tech AS
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
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/awriter"
	"github.com/mendersoftware/mender-artifact/handlers"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstall(t *testing.T) {
	art, art_size, err := MakeRootfsImageArtifact(1, false, false, "test update")
	assert.NoError(t, err)
	assert.NotNil(t, art)

	// image not compatible with device
	err = Install(art, art_size, "fake-device", nil, "", nil, true, nil)
	assert.Error(t, err)
	assert.Contains(t, errors.Cause(err).Error(),
		"not compatible with device fake-device")

	art, art_size, err = MakeRootfsImageArtifact(1, false, false, "test update")
	assert.NoError(t, err)
	err = Install(art, art_size, "vexpress-qemu", nil, "", new(fDevice), true, nil)
	assert.NoError(t, err)
}

// InstallationStateStore
type testStateStore struct {
	stuff map[string][]byte
}

func newTestStateStore() *testStateStore {
	return &testStateStore{
		stuff: make(map[string][]byte),
	}
}

func (tss *testStateStore) ReadAll(name string) ([]byte, error) {
	if val, ok := tss.stuff[name]; ok {
		return val, nil
	} else {
		return nil, errors.Errorf("key %s not found", name)
	}
}
func (tss *testStateStore) WriteAll(name string, data []byte) error {
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	tss.stuff[name] = data
	return nil
}
func (tss *testStateStore) Remove(name string) error {
	delete(tss.stuff, name)
	return nil
}
func (tss *testStateStore) Close() error {
	return nil
}

type LimitedFile struct {
	*os.File
	errToReturn           error
	returnErrorAfterBytes int64
}

func (lf *LimitedFile) Read(buf []byte) (int, error) {
	currentOffset, err := lf.File.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}

	if currentOffset == lf.returnErrorAfterBytes {
		return 0, lf.errToReturn
	}

	bytesToRead := len(buf)
	if int64(bytesToRead)+currentOffset > lf.returnErrorAfterBytes {
		bytesToRead = int(lf.returnErrorAfterBytes - int64(currentOffset))
	}

	return lf.File.Read(buf[:bytesToRead])
}

func ReadersEqual(r1, r2 io.Reader) (bool, error) {
	b1 := &bytes.Buffer{}
	b2 := &bytes.Buffer{}
	chunkSize := int64(4096)

	for {

		b1.Reset()
		b2.Reset()

		read1, err1 := io.CopyN(b1, r1, chunkSize)
		read2, err2 := io.CopyN(b2, r2, chunkSize)

		if read1 != read2 {
			return false, nil
		}

		if bytes.Compare(b1.Bytes(), b2.Bytes()) != 0 {
			return false, nil
		}

		if err1 != nil || err2 != nil {
			eof1 := err1 == io.EOF
			eof2 := err2 == io.EOF
			return eof1 && eof2, nil
		}

	}
}

func TestReadersEqual(t *testing.T) {

	table := []struct {
		r1, r2      io.Reader
		shouldMatch bool
	}{
		{
			r1:          bytes.NewReader([]byte("testing")),
			r2:          bytes.NewReader([]byte("testing")),
			shouldMatch: true,
		},
		{
			r1:          bytes.NewReader([]byte("terting")),
			r2:          bytes.NewReader([]byte("testing")),
			shouldMatch: false,
		},
		{
			r1:          bytes.NewReader([]byte("tersting")),
			r2:          bytes.NewReader([]byte("testing")),
			shouldMatch: false,
		},
		{
			r1:          bytes.NewReader([]byte("")),
			r2:          bytes.NewReader([]byte("t")),
			shouldMatch: false,
		},
		{
			r1:          bytes.NewReader([]byte("")),
			r2:          bytes.NewReader([]byte("")),
			shouldMatch: true,
		},
	}

	for idx, tt := range table {
		t.Run(fmt.Sprintf("test%02d", idx), func(t *testing.T) {
			eq, err := ReadersEqual(tt.r1, tt.r2)
			assert.NoError(t, err)
			assert.Equal(t, tt.shouldMatch, eq, "ReadersEqual returned the wrong verdict")
		})
	}
}

func TestInstallWithCheckpointing(t *testing.T) {

	// Artifact size needs to be large enough that the gzip stream is actually checkpointable
	sb := strings.Builder{}
	for idx := 0; idx < 100000; idx++ {
		_, err := sb.WriteString(fmt.Sprintf("%d ", idx))
		require.NoError(t, err)
	}
	rootfsData := sb.String()

	art, art_size, err := MakeRootfsImageArtifact(2, false, false, rootfsData)
	assert.NoError(t, err)

	tempDir, err := ioutil.TempDir("", "test_with_checkpointing")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Store artifact in a temp file so we can access it more than once, Seek(), etc.
	artifactFile, err := ioutil.TempFile(tempDir, "artifactfile")
	require.NoError(t, err)
	artifactPath := artifactFile.Name()
	_, err = io.CopyN(artifactFile, art, art_size)
	require.NoError(t, err)
	err = artifactFile.Close()
	require.NoError(t, err)

	fd2, err := newfDevice2(tempDir)
	require.NoError(t, err)

	installStateStore := newTestStateStore()

	art_file, err := os.Open(artifactPath)
	require.NoError(t, err)

	artLimitedReader := &LimitedFile{
		File:                  art_file,
		errToReturn:           errors.Errorf("something killed the install"),
		returnErrorAfterBytes: 65 * 1024,
	}

	err = Install(artLimitedReader, art_size, "vexpress-qemu", nil, "", fd2, true, installStateStore)
	assert.Error(t, err, "installation was supposed to fail but it didn't?")

	art_file.Close()
	artLimitedReader = nil

	assert.True(t, len(installStateStore.stuff) == 1, "installStateStore never got any checkpoints")

	// verify that rootfs was not fully installed to block device
	checkBlockDevFile, err := os.Open(fd2.fakeBlockDevice.Name())
	rootfsIsCorrect, err := ReadersEqual(
		bytes.NewReader([]byte(rootfsData)),
		checkBlockDevFile,
	)
	checkBlockDevFile.Close()
	require.NoError(t, err)
	assert.False(t, rootfsIsCorrect, "rootfs was completely installed but we expected install to be interrupted")

	// reopen art_file
	art_file, err = os.Open(artifactPath)
	require.NoError(t, err)

	err = Install(art_file, art_size, "vexpress-qemu", nil, "", fd2, true, installStateStore)
	assert.NoError(t, err, "installation was supposed to succeed but it failed")

	art_file.Close()

	// verify that rootfs has now been fullyk installed to block device
	checkBlockDevFile, err = os.Open(fd2.fakeBlockDevice.Name())
	rootfsIsCorrect, err = ReadersEqual(
		bytes.NewReader([]byte(rootfsData)),
		checkBlockDevFile,
	)
	checkBlockDevFile.Close()
	require.NoError(t, err)
	assert.True(t, rootfsIsCorrect, "rootfs was not completely installed")
}

func TestInstallSigned(t *testing.T) {
	art, art_size, err := MakeRootfsImageArtifact(2, true, false, "test update")
	assert.NoError(t, err)
	assert.NotNil(t, art)

	// no key for verifying artifact
	art, art_size, err = MakeRootfsImageArtifact(2, true, false, "test update")
	assert.NoError(t, err)
	err = Install(art, art_size, "vexpress-qemu", nil, "", new(fDevice), true, nil)
	assert.NoError(t, err)

	// image not compatible with device
	art, art_size, err = MakeRootfsImageArtifact(2, true, false, "test update")
	assert.NoError(t, err)
	err = Install(art, art_size, "fake-device", []byte(PublicRSAKey), "", new(fDevice), true, nil)
	assert.Error(t, err)
	assert.Contains(t, errors.Cause(err).Error(),
		"not compatible with device fake-device")

	// installation successful
	art, art_size, err = MakeRootfsImageArtifact(2, true, false, "test update")
	assert.NoError(t, err)
	err = Install(art, art_size, "vexpress-qemu", []byte(PublicRSAKey), "", new(fDevice), true, nil)
	assert.NoError(t, err)

	// have a key but artifact is v1
	art, art_size, err = MakeRootfsImageArtifact(1, false, false, "test update")
	assert.NoError(t, err)
	err = Install(art, art_size, "vexpress-qemu", []byte(PublicRSAKey), "", new(fDevice), true, nil)
	assert.Error(t, err)
}

func TestInstallNoSignature(t *testing.T) {
	art, art_size, err := MakeRootfsImageArtifact(2, false, false, "test update")
	assert.NoError(t, err)
	assert.NotNil(t, art)

	// image does not contain signature
	err = Install(art, art_size, "vexpress-qemu", []byte(PublicRSAKey), "", new(fDevice), true, nil)
	assert.Error(t, err)
	assert.Contains(t, errors.Cause(err).Error(),
		"expecting signed artifact, but no signature file found")
}

func TestInstallWithScripts(t *testing.T) {
	art, art_size, err := MakeRootfsImageArtifact(2, false, true, "test update")
	assert.NoError(t, err)
	assert.NotNil(t, art)

	scrDir, err := ioutil.TempDir("", "test_scripts")
	assert.NoError(t, err)
	defer os.RemoveAll(scrDir)

	err = Install(art, art_size, "vexpress-qemu", nil, scrDir, new(fDevice), true, nil)
	assert.NoError(t, err)
}

type fDevice struct{}

func (d *fDevice) InstallUpdate(r io.ReadCloser, l int64, initialOffset int64, ipc InstallationProgressConsumer) error {
	_, err := io.Copy(ioutil.Discard, r)
	return err
}

func (d *fDevice) VerifyUpdatedPartition(size int64, expectedSHA256Checksum []byte) error { return nil }

func (d *fDevice) EnableUpdatedPartition() error { return nil }

type fDevice2 struct {
	fDevice
	fakeBlockDevice *os.File
	callbackEvery   int64
}

func newfDevice2(tempDir string) (*fDevice2, error) {

	f, err := ioutil.TempFile(tempDir, "pretend-block-device")
	if err != nil {
		return nil, err
	}

	fd2 := &fDevice2{
		fakeBlockDevice: f,
		callbackEvery:   64,
	}

	return fd2, nil
}

func (d *fDevice2) InstallUpdate(r io.ReadCloser, l int64, initialOffset int64, ipc InstallationProgressConsumer) error {
	_, err := d.fakeBlockDevice.Seek(initialOffset, io.SeekStart)
	if err != nil {
		return err
	}

	bytesToCopy := l - initialOffset
	for bytesToCopy > 0 {

		offsetBefore, err := d.fakeBlockDevice.Seek(0, io.SeekCurrent)
		if err != nil {
			return err // shouldn't happen
		}

		bytesCopied, err := io.CopyN(d.fakeBlockDevice, r, d.callbackEvery)

		bytesToCopy -= bytesCopied

		if ipc != nil {
			if bytesCopied > 0 {
				ipc.UpdateInstallationProgress(offsetBefore, offsetBefore+bytesCopied)
			}
		}

		if err != nil {
			if err == io.EOF {
				if bytesToCopy == 0 {
					err = nil
				} else {
					err = io.ErrUnexpectedEOF
				}
			}
			return err
		}
	}
	return nil
}

func (d *fDevice2) Close() error {
	return d.fakeBlockDevice.Close()
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
)

func MakeRootfsImageArtifact(version int, signed bool,
	hasScripts bool, rootfsData string) (io.ReadCloser, int64, error) {
	upd, err := MakeFakeUpdate(rootfsData)
	if err != nil {
		return nil, 0, err
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

	scr := artifact.Scripts{}
	if hasScripts {
		s, ferr := ioutil.TempFile("", "ArtifactInstall_Enter_10_")
		if ferr != nil {
			return nil, 0, err
		}
		defer os.Remove(s.Name())

		_, err = io.WriteString(s, "execute me!")

		if err = scr.Add(s.Name()); err != nil {
			return nil, 0, err
		}
	}

	updates := &awriter.Updates{U: []handlers.Composer{u}}
	err = aw.WriteArtifact("mender", version, []string{"vexpress-qemu"},
		"mender-1.1", updates, &scr)
	if err != nil {
		return nil, 0, err
	}
	return &rc{art}, int64(art.Len()), nil
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

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
package tests

import (
	"bytes"
	"io/ioutil"
	"os"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender-artifact/awriter"
	"github.com/mendersoftware/mender-artifact/handlers"
)

type ArtifactDepends artifact.ArtifactDepends
type ArtifactProvides artifact.ArtifactProvides

type artifactReadCloser struct {
	*bytes.Reader
}

func (rc *artifactReadCloser) Close() error {
	return nil
}

// Creates a test rootfs artifact (version 3) with rootfs content @data, and the
// provided depends and provides.
func CreateTestArtifactV3(data, compressAlgorithm string,
	artifactProvides *ArtifactProvides, artifactDepends *ArtifactDepends,
	typeProvides map[string]string,
	typeDepends map[string]interface{}) (*artifactReadCloser, error) {
	var artifactArgs *awriter.WriteArtifactArgs
	if artifactProvides == nil {
		artifactProvides = &ArtifactProvides{
			ArtifactName: "TestName",
		}
	}
	var compressor artifact.Compressor
	switch compressAlgorithm {
	case "gzip":
		compressor = artifact.NewCompressorGzip()
	case "lzma":
		compressor = artifact.NewCompressorLzma()
	default:
		compressor = artifact.NewCompressorNone()
	}
	buf := bytes.NewBuffer(nil)
	artifactWriter := awriter.NewWriter(buf, compressor)
	updateFile, err := createFakeUpdateFile(data)
	if err != nil {
		return nil, err
	}
	defer os.Remove(updateFile)
	u := handlers.NewRootfsV3(updateFile)

	artifactType := "rootfs-image"

	artifactArgs = &awriter.WriteArtifactArgs{
		Format:   "mender",
		Version:  3,
		Provides: (*artifact.ArtifactProvides)(artifactProvides),
		Depends:  (*artifact.ArtifactDepends)(artifactDepends),
		TypeInfoV3: &artifact.TypeInfoV3{
			Type:             &artifactType,
			ArtifactProvides: (artifact.TypeInfoProvides)(typeProvides),
			ArtifactDepends:  (artifact.TypeInfoDepends)(typeDepends),
		},
		Updates: &awriter.Updates{Updates: []handlers.Composer{u}},
	}
	err = artifactWriter.WriteArtifact(artifactArgs)
	if err != nil {
		return nil, err
	}
	bufReadCloser := &artifactReadCloser{bytes.NewReader(buf.Bytes())}
	return bufReadCloser, nil
}

// Creates a test rootfs artifact (version 2) with rootfs-content @data, and the
// provided depends and provides.
func CreateTestArtifactV2(data, compressAlgorithm, artifactName string,
	compatDevices []string) (*artifactReadCloser, error) {
	var artifactArgs *awriter.WriteArtifactArgs
	updateFile, err := createFakeUpdateFile(data)
	if err != nil {
		return nil, err
	}
	var compressor artifact.Compressor
	switch compressAlgorithm {
	case "gzip":
		compressor = artifact.NewCompressorGzip()
	case "lzma":
		compressor = artifact.NewCompressorLzma()
	default:
		compressor = artifact.NewCompressorNone()
	}
	defer os.Remove(updateFile)
	u := handlers.NewRootfsV2(updateFile)
	artifactArgs = &awriter.WriteArtifactArgs{
		Devices: compatDevices,
		Format:  "mender",
		Name:    artifactName,
		Updates: &awriter.Updates{Updates: []handlers.Composer{u}},
		Version: 2,
	}
	buf := bytes.NewBuffer(nil)
	artifactWriter := awriter.NewWriter(buf, compressor)

	err = artifactWriter.WriteArtifact(artifactArgs)
	if err != nil {
		return nil, err
	}

	bufReadCloser := &artifactReadCloser{bytes.NewReader(buf.Bytes())}
	return bufReadCloser, nil
}

func createFakeUpdateFile(content string) (string, error) {
	f, err := ioutil.TempFile("", "test_update")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if len(content) > 0 {
		if _, err := f.WriteString(content); err != nil {
			return "", err
		}
	}
	return f.Name(), nil
}

func CreateTestBootstrapArtifact(
	t *testing.T,
	path, device_type, artifactName string,
	artifactProvides artifact.TypeInfoProvides,
	clearsArtifactProvides []string,
) {
	comp := artifact.NewCompressorNone()
	updates := &awriter.Updates{
		Updates: []handlers.Composer{handlers.NewBootstrapArtifact()},
	}

	f, err := os.Create(path)
	require.NoError(t, err)
	aw := awriter.NewWriter(f, comp)

	err = aw.WriteArtifact(&awriter.WriteArtifactArgs{
		Format:  "mender",
		Version: 3,
		Devices: []string{device_type},
		Name:    artifactName,
		Updates: updates,
		Scripts: nil,
		Provides: &artifact.ArtifactProvides{
			ArtifactName: artifactName,
		},
		Depends: &artifact.ArtifactDepends{
			CompatibleDevices: []string{device_type},
		},
		TypeInfoV3: &artifact.TypeInfoV3{
			ArtifactProvides:       artifactProvides,
			ClearsArtifactProvides: clearsArtifactProvides,
		},
	})
	require.NoError(t, err)
	log.Infof("Written %s", path)
}

func CreateTestBootstrapArtifactDefault(t *testing.T, path string) {
	CreateTestBootstrapArtifact(
		t,
		path,
		"foo-bar",
		"bootstrap-stuff",
		artifact.TypeInfoProvides{"something": "cool"},
		[]string{"something", "artifact_name"},
	)
}

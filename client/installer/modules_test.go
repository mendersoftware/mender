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

package installer

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mendersoftware/mender-artifact/artifact"
	"github.com/mendersoftware/mender/common/system"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testStreamsTreeInfo struct {
	typeInfo *artifact.TypeInfoV3
}

func (i *testStreamsTreeInfo) GetCurrentArtifactName() (string, error) {
	return "test-name", nil
}

func (i *testStreamsTreeInfo) GetCurrentArtifactGroup() (string, error) {
	return "test-group", nil
}

func (i *testStreamsTreeInfo) GetDeviceType() (string, error) {
	return "test-device", nil
}

func (i *testStreamsTreeInfo) GetVersion() int {
	return 3
}

func (i *testStreamsTreeInfo) GetUpdateType() string {
	return "test-type"
}

func (i *testStreamsTreeInfo) GetUpdateOriginalType() string {
	return "test-type"
}

func (i *testStreamsTreeInfo) GetUpdateDepends() (artifact.TypeInfoDepends, error) {
	return i.GetUpdateOriginalDepends(), nil
}
func (i *testStreamsTreeInfo) GetUpdateProvides() (artifact.TypeInfoProvides, error) {
	return i.GetUpdateOriginalProvides(), nil
}
func (i *testStreamsTreeInfo) GetUpdateMetaData() (map[string]interface{}, error) {
	return i.GetUpdateOriginalMetaData(), nil
}
func (i *testStreamsTreeInfo) GetUpdateClearsProvides() []string {
	return i.GetUpdateOriginalClearsProvides()
}

func (i *testStreamsTreeInfo) GetUpdateOriginalDepends() artifact.TypeInfoDepends {
	return i.typeInfo.ArtifactDepends
}
func (i *testStreamsTreeInfo) GetUpdateOriginalProvides() artifact.TypeInfoProvides {
	return i.typeInfo.ArtifactProvides
}
func (i *testStreamsTreeInfo) GetUpdateOriginalMetaData() map[string]interface{} {
	return map[string]interface{}{
		"testKey": "testValue",
	}
}
func (i *testStreamsTreeInfo) GetUpdateOriginalClearsProvides() []string {
	return i.typeInfo.ClearsArtifactProvides
}

func (i *testStreamsTreeInfo) GetUpdateAugmentDepends() artifact.TypeInfoDepends {
	return nil
}
func (i *testStreamsTreeInfo) GetUpdateAugmentProvides() artifact.TypeInfoProvides {
	return nil
}
func (i *testStreamsTreeInfo) GetUpdateAugmentMetaData() map[string]interface{} {
	return map[string]interface{}{}
}
func (i *testStreamsTreeInfo) GetUpdateAugmentClearsProvides() []string {
	return nil
}

func (i *testStreamsTreeInfo) GetUpdateOriginalTypeInfoWriter() io.Writer {
	return i.typeInfo
}
func (i *testStreamsTreeInfo) GetUpdateAugmentTypeInfoWriter() io.Writer {
	return nil
}

func verifyFileContent(t *testing.T, path, content string) {
	fd, err := os.Open(path)
	require.NoError(t, err)
	defer fd.Close()

	stat, err := fd.Stat()
	require.NoError(t, err)

	buf := make([]byte, stat.Size())
	n, err := fd.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, len(content), n)

	assert.Equal(t, content, string(buf))
}

func verifyFileJSON(t *testing.T, path, content string) {
	fd, err := os.Open(path)
	require.NoError(t, err)
	defer fd.Close()

	stat, err := fd.Stat()
	require.NoError(t, err)

	buf := make([]byte, stat.Size())
	_, err = fd.Read(buf)
	require.NoError(t, err)

	var decoded interface{}
	err = json.Unmarshal(buf, &decoded)
	assert.NoError(t, err)

	var expected interface{}
	err = json.Unmarshal([]byte(content), &expected)
	require.NoError(t, err)

	assert.Truef(t, reflect.DeepEqual(expected, decoded),
		"Expected: '%s'\nActual: '%s'", string(content), string(buf))
}

func TestStreamsTree(t *testing.T) {
	tmpdir, err := ioutil.TempDir("", "")
	require.NoError(t, err)
	defer os.RemoveAll(tmpdir)

	treedir := path.Join(tmpdir, "payloads", "0000", "tree")

	prov := artifact.ArtifactProvides{
		ArtifactName:  "new-artifact",
		ArtifactGroup: "new-group",
	}
	dep := artifact.ArtifactDepends{
		ArtifactName:      []string{"name1", "name2"},
		CompatibleDevices: []string{"test-device"},
		ArtifactGroup:     []string{"existing-group"},
	}

	headerInfo := artifact.NewHeaderInfoV3([]artifact.UpdateType{
		artifact.UpdateType{
			Type: "test-type",
		},
	}, &prov, &dep)

	i := testStreamsTreeInfo{
		typeInfo: &artifact.TypeInfoV3{
			Type: "test-type",
			ArtifactDepends: artifact.TypeInfoDepends{
				"test-depend-key": "test-depend-value",
			},
			ArtifactProvides: artifact.TypeInfoProvides{
				"test-provide-key": "test-provide-value",
			},
		},
	}

	mod := ModuleInstaller{
		payloadIndex:    0,
		modulesWorkPath: tmpdir,
		updateType:      "test-type",
		artifactInfo:    &i,
		deviceInfo:      &i,
	}

	err = mod.buildStreamsTree(headerInfo, nil, &i)
	require.NoError(t, err)

	verifyFileContent(t, path.Join(treedir, "version"), "3")
	verifyFileContent(t, path.Join(treedir, "current_artifact_group"), "test-group")
	verifyFileContent(t, path.Join(treedir, "current_artifact_name"), "test-name")
	verifyFileContent(t, path.Join(treedir, "current_device_type"), "test-device")

	verifyFileContent(t, path.Join(treedir, "header", "artifact_group"),
		"new-group")
	verifyFileContent(t, path.Join(treedir, "header", "artifact_name"),
		"new-artifact")
	verifyFileContent(t, path.Join(treedir, "header", "payload_type"),
		"test-type")

	verifyFileJSON(t, path.Join(treedir, "header", "header-info"), `{
  "payloads": [
    {
      "type": "test-type"
    }
  ],
  "artifact_provides": {
    "artifact_name": "new-artifact",
    "artifact_group": "new-group"
  },
  "artifact_depends": {
    "artifact_name": [
      "name1",
      "name2"
    ],
    "device_type": [
      "test-device"
    ],
    "artifact_group": [
      "existing-group"
    ]
  }
}`)
	verifyFileJSON(t, path.Join(treedir, "header", "type-info"), `{
  "type": "test-type",
  "artifact_provides": {
    "test-provide-key": "test-provide-value"
  },
  "artifact_depends": {
    "test-depend-key": "test-depend-value"
  }
}`)
	verifyFileJSON(t, path.Join(treedir, "header", "meta-data"), `{
  "testKey": "testValue"
}`)

	stat, err := os.Stat(path.Join(treedir, "tmp"))
	require.NoError(t, err)
	assert.True(t, stat.IsDir())

	dirlist, err := ioutil.ReadDir(path.Join(treedir, "tmp"))
	require.NoError(t, err)
	assert.Equal(t, 0, len(dirlist))
}

func moduleDownloadSetup(t *testing.T, tmpdir, helperArg string) (*moduleDownload, *delayKiller) {
	require.NoError(t, os.MkdirAll(path.Join(tmpdir, "streams"), 0700))
	require.NoError(t, os.MkdirAll(path.Join(tmpdir, "tmp"), 0700))
	require.NoError(t, syscall.Mkfifo(path.Join(tmpdir, "stream-next"), 0600))

	cwd, err := os.Getwd()
	require.NoError(t, err)

	cmd := system.Command(path.Join(cwd, "modules_test_helper.sh"), helperArg)
	cmd.Dir = tmpdir
	// Create new process group so we can kill them all instead of just the parent.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	require.NoError(t, cmd.Start())
	delayKiller := newDelayKiller(cmd.Process, 5*time.Second, time.Second)

	download := newModuleDownload(tmpdir, cmd)
	go download.detachedDownloadProcess()

	return download, delayKiller
}

type modulesDownloadTestCase struct {
	testName  string
	scriptArg string

	// Must be same length
	streamContents []string
	streamNames    []string
	verifyFiles    []string

	verifyNotExist []string
	remove         []string
	create         []string
	downloadErr    []error
	finishErr      error
}

var modulesDownloadTestCases []modulesDownloadTestCase = []modulesDownloadTestCase{
	modulesDownloadTestCase{
		testName:       "Mender download",
		scriptArg:      "menderDownload",
		streamContents: []string{"Test content"},
		streamNames:    []string{"test-name"},
		verifyFiles:    []string{"files/test-name"},
	},
	modulesDownloadTestCase{
		testName:       "Module download",
		scriptArg:      "moduleDownload",
		streamContents: []string{"Test content"},
		streamNames:    []string{"test-name"},
		verifyFiles:    []string{"tmp/module-downloaded-file0"},
		// Check that Mender doesn't also create the file.
		verifyNotExist: []string{"files/test-name"},
	},
	modulesDownloadTestCase{
		testName:       "Module download failure",
		scriptArg:      "moduleDownloadFailure",
		streamContents: []string{"Test content"},
		streamNames:    []string{"test-name"},
		verifyNotExist: []string{"files/test-name"},
		downloadErr:    []error{errors.New("Update module terminated abnormally: exit status 1")},
	},
	modulesDownloadTestCase{
		testName:       "Module download short read of stream-next",
		scriptArg:      "moduleDownloadStreamNextShortRead",
		streamContents: []string{"Test content"},
		streamNames:    []string{"test-name"},
		verifyNotExist: []string{"files/test-name"},
		downloadErr:    []error{errors.New("Update module terminated in the middle of the download")},
	},
	modulesDownloadTestCase{
		testName:  "Module download short read of stream",
		scriptArg: "moduleDownloadStreamShortRead",
		// Because of buffering, we need a long string to detect short
		// reads.
		streamContents: []string{strings.Repeat("0", 10000000)},
		streamNames:    []string{"test-name"},
		verifyNotExist: []string{"files/test-name"},
		downloadErr:    []error{errors.New("broken pipe")},
	},
	modulesDownloadTestCase{
		testName:       "Cannot open stream file",
		scriptArg:      "moduleDownload",
		streamContents: []string{"Test content"},
		streamNames:    []string{"test-name"},
		verifyNotExist: []string{"files/test-name"},
		remove:         []string{"streams"},
		downloadErr:    []error{errors.New("no such file or directory")},
	},
	modulesDownloadTestCase{
		testName:       "files dir blocked by file",
		scriptArg:      "menderDownload",
		streamContents: []string{"Test content"},
		streamNames:    []string{"test-name"},
		create:         []string{"files"},
		downloadErr:    []error{errors.New("file exists")},
	},
	modulesDownloadTestCase{
		testName:       "Mender download multiple files",
		scriptArg:      "menderDownload",
		streamContents: []string{"Test content", "more content"},
		streamNames:    []string{"test-name", "another-name"},
		verifyFiles:    []string{"files/test-name", "files/another-name"},
	},
	modulesDownloadTestCase{
		testName:       "Module download multiple files",
		scriptArg:      "moduleDownload",
		streamContents: []string{"Test content", "more content"},
		streamNames:    []string{"test-name", "another-name"},
		verifyFiles:    []string{"tmp/module-downloaded-file0", "tmp/module-downloaded-file1"},
	},
	modulesDownloadTestCase{
		testName:       "Module download multiple files, downloads only one",
		scriptArg:      "moduleDownloadOnlyOne",
		streamContents: []string{"Test content", "more content"},
		streamNames:    []string{"test-name", "another-name"},
		downloadErr:    []error{nil, errors.New("Update module terminated in the middle of the download")},
	},
	modulesDownloadTestCase{
		testName:       "Module downloads two entries, but one file",
		scriptArg:      "moduleDownloadTwoEntriesOneFile",
		streamContents: []string{"Test content", "more content"},
		streamNames:    []string{"test-name", "another-name"},
		downloadErr:    []error{nil, errors.New("Update module terminated in the middle of the download")},
	},
	modulesDownloadTestCase{
		testName:       "Module download doesn't read final zero entry",
		scriptArg:      "moduleDownloadNoZeroEntry",
		streamContents: []string{"Test content"},
		streamNames:    []string{"test-name"},
	},
	modulesDownloadTestCase{
		testName:       "Module download fails final exit",
		scriptArg:      "moduleDownloadFailExit",
		streamContents: []string{"Test content"},
		streamNames:    []string{"test-name"},
		finishErr:      errors.New("Update module terminated abnormally: exit status 1"),
	},
	modulesDownloadTestCase{
		testName:       "Module download hangs",
		scriptArg:      "moduleDownloadHang",
		streamContents: []string{"Test content"},
		streamNames:    []string{"test-name"},
		downloadErr:    []error{errors.New("Update module terminated abnormally: signal: terminated")},
	},
	modulesDownloadTestCase{
		testName:       "Module download never exits",
		scriptArg:      "moduleDownloadExitHang",
		streamContents: []string{"Test content"},
		streamNames:    []string{"test-name"},
		finishErr:      errors.New("Update module terminated abnormally: signal: terminated"),
	},
}

func TestModulesDownload(t *testing.T) {
	for _, c := range modulesDownloadTestCases {
		t.Run(c.testName, func(t *testing.T) {
			subTestModulesDownload(t, &c)
		})
	}
}

func assertIsError(t *testing.T, expected, actual error) {
	if expected == nil {
		assert.NoError(t, actual)
	} else {
		assert.Error(t, actual)
		if actual != nil {
			assert.Contains(t, actual.Error(), expected.Error())
		}
	}
}

func subTestModulesDownload(t *testing.T, c *modulesDownloadTestCase) {
	tmpdir, err := ioutil.TempDir("", "TestModuleDownload")
	require.NoError(t, err)
	defer os.RemoveAll(tmpdir)

	goRoutines := runtime.NumGoroutine()

	download, delayKiller := moduleDownloadSetup(t, tmpdir, c.scriptArg)

	for _, file := range c.remove {
		require.NoError(t, os.RemoveAll(path.Join(tmpdir, file)))
	}
	for _, file := range c.create {
		fd, err := os.OpenFile(path.Join(tmpdir, file), os.O_WRONLY|os.O_CREATE, 0600)
		require.NoError(t, err)
		fd.Close()
	}

	for n := range c.streamContents {
		buf := bytes.NewBuffer([]byte(c.streamContents[n]))
		err = download.downloadStream(buf, c.streamNames[n])
		if n < len(c.downloadErr) {
			assertIsError(t, c.downloadErr[n], err)
		} else {
			assert.NoError(t, err)
		}
	}

	err = download.finishDownloadProcess()
	assertIsError(t, c.finishErr, err)
	delayKiller.Stop()

	for n := range c.verifyFiles {
		verifyFileContent(t, path.Join(tmpdir, c.verifyFiles[n]), c.streamContents[n])
	}

	for _, file := range c.verifyNotExist {
		_, err = os.Stat(path.Join(tmpdir, file))
		assert.True(t, os.IsNotExist(err))
	}

	// Make sure the downloader didn't leak any Go routines.
	runtime.GC()
	ok := false
	for c := 0; c < 5; c++ {
		if goRoutines == runtime.NumGoroutine() {
			ok = true
			break
		}
		// The finalizer may need some CPU time to run.
		time.Sleep(time.Second)
	}
	assert.True(t, ok, "Downloader leaked go routines")
}

// Verify the clears_artifact_provides attribute specifically, since this was
// added in Mender client 2.5.
func TestStreamsTreeClearsProvidesAttribute(t *testing.T) {
	tmpdir, err := ioutil.TempDir("", "")
	require.NoError(t, err)
	defer os.RemoveAll(tmpdir)

	treedir := path.Join(tmpdir, "payloads", "0000", "tree")

	prov := artifact.ArtifactProvides{
		ArtifactName: "new-artifact",
	}
	dep := artifact.ArtifactDepends{
		ArtifactName:      []string{"name1", "name2"},
		CompatibleDevices: []string{"test-device"},
	}

	headerInfo := artifact.NewHeaderInfoV3([]artifact.UpdateType{
		artifact.UpdateType{
			Type: "test-type",
		},
	}, &prov, &dep)

	i := testStreamsTreeInfo{
		typeInfo: &artifact.TypeInfoV3{
			Type: "test-type",
			ArtifactDepends: artifact.TypeInfoDepends{
				"test-depend-key": "test-depend-value",
			},
			ArtifactProvides: artifact.TypeInfoProvides{
				"test-provide-key": "test-provide-value",
			},
			ClearsArtifactProvides: []string{
				"rootfs-image.*",
				"my-fs.my-app.*",
			},
		},
	}

	mod := ModuleInstaller{
		payloadIndex:    0,
		modulesWorkPath: tmpdir,
		updateType:      "test-type",
		artifactInfo:    &i,
		deviceInfo:      &i,
	}

	err = mod.buildStreamsTree(headerInfo, nil, &i)
	require.NoError(t, err)

	verifyFileJSON(t, path.Join(treedir, "header", "type-info"), `{
  "type": "test-type",
  "artifact_provides": {
    "test-provide-key": "test-provide-value"
  },
  "artifact_depends": {
    "test-depend-key": "test-depend-value"
  },
  "clears_artifact_provides": [
    "rootfs-image.*",
    "my-fs.my-app.*"
  ]
}`)
}

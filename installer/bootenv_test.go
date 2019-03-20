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
	"testing"

	"github.com/mendersoftware/mender/system"
	stest "github.com/mendersoftware/mender/system/testing"
	"github.com/stretchr/testify/assert"
)

//if no config file is present
//Cannot parse config file: No such file or directory

//no valid device in config file
//Cannot access MTD device /mnt/uboot.env: No such file or directory

//fw_printenv
//arch=arm
//baudrate=115200
//board=rpi

//fw_printenv arch
//arch=arm

//fw_printenv non_existing_var
//## Error: "non_existing_var" not defined

//fw_printenv
//Warning: Bad CRC, using default environment
//bootcmd=run distro_bootcmd
//bootdelay=2

//fw_setenv name value
//this prints nothing on success just returns 0

//fw_setenv name value
//Cannot access MTD device /mnt/uboot.env: No such file or directory
//Error: environment not initialized

//fw_setenv name
//this removes env variable; prints nothing on success just returns 0

func Test_EnvWrite_OSResponseOK_WritesOK(t *testing.T) {
	runner := stest.NewTestOSCalls("", 0)

	fakeEnv := UBootEnv{runner}
	if err := fakeEnv.WriteEnv(BootVars{"bootcnt": "3"}); err != nil {
		t.FailNow()
	}
}

func Test_EnvWrite_OSResponseError_Fails(t *testing.T) {
	runner := stest.NewTestOSCalls("", 1)
	fakeEnv := UBootEnv{runner}
	if err := fakeEnv.WriteEnv(BootVars{"bootcnt": "3"}); err == nil {
		t.FailNow()
	}

	runner = stest.NewTestOSCalls("Cannot parse config file: No such file or directory\n", 1)
	if err := fakeEnv.WriteEnv(BootVars{"bootcnt": "3"}); err == nil {
		t.FailNow()
	}
}

func Test_EnvRead_HaveVariable_ReadsVariable(t *testing.T) {
	runner := stest.NewTestOSCalls("arch=arm", 0)
	fakeEnv := UBootEnv{runner}

	variables, err := fakeEnv.ReadEnv("arch")
	if err != nil || variables["arch"] != "arm" {
		t.FailNow()
	}

	// test reading multiple variables
	runner = stest.NewTestOSCalls("var1=1\nvar2=2", 0)
	fakeEnv = UBootEnv{runner}

	variables, err = fakeEnv.ReadEnv("var1", "var2")
	if err != nil || variables["var1"] != "1" || variables["var2"] != "2" {
		t.FailNow()
	}

	// test multiple blank lines in output
	runner = stest.NewTestOSCalls("arch=arm\n\n\n", 0)
	fakeEnv = UBootEnv{runner}

	variables, err = fakeEnv.ReadEnv("arch")
	if err != nil || variables["arch"] != "arm" {
		t.FailNow()
	}
}

func Test_EnvRead_HaveEnvWarning_FailsReading(t *testing.T) {
	runner := stest.NewTestOSCalls("Warning: Bad CRC, using default environment\nvar=1\n", 0)
	fakeEnv := UBootEnv{runner}

	variables, err := fakeEnv.ReadEnv("var")
	if err == nil || variables != nil {
		t.FailNow()
	}

	runner = stest.NewTestOSCalls("Warning: Bad CRC, using default environment\nvar=1\n", 1)

	variables, err = fakeEnv.ReadEnv("var")
	if err == nil || variables != nil {
		t.FailNow()
	}
}

func Test_EnvRead_NonExisting_FailsReading(t *testing.T) {
	runner := stest.NewTestOSCalls("## Error: \"non_existing_var\" not defined\n", 0)
	fakeEnv := UBootEnv{runner}

	variables, err := fakeEnv.ReadEnv("non_existing_var")
	if err == nil || variables != nil {
		t.FailNow()
	}

	runner = stest.NewTestOSCalls("## Error: \"non_existing_var\" not defined\n", 1)

	variables, err = fakeEnv.ReadEnv("non_existing_var")
	if err == nil || variables != nil {
		t.FailNow()
	}
}

func Test_EnvCanary(t *testing.T) {
	runner := stest.NewTestOSCalls("var=1\nmender_check_saveenv_canary=1\nmender_saveenv_canary=0\n", 0)
	fakeEnv := UBootEnv{runner}
	variables, err := fakeEnv.ReadEnv("var")
	assert.Error(t, err)

	runner = stest.NewTestOSCalls("var=1\nmender_check_saveenv_canary=1\n", 0)
	fakeEnv = UBootEnv{runner}
	variables, err = fakeEnv.ReadEnv("var")
	assert.Error(t, err)

	runner = stest.NewTestOSCalls("var=1\nmender_check_saveenv_canary=1\nmender_saveenv_canary=1\n", 0)
	fakeEnv = UBootEnv{runner}
	variables, err = fakeEnv.ReadEnv("var")
	assert.NoError(t, err)
	assert.Equal(t, variables["var"], "1")

	runner = stest.NewTestOSCalls("var=1\nmender_check_saveenv_canary=0\n", 0)
	fakeEnv = UBootEnv{runner}
	variables, err = fakeEnv.ReadEnv("var")
	assert.NoError(t, err)
	assert.Equal(t, variables["var"], "1")

	runner = stest.NewTestOSCalls("var=1\n", 0)
	fakeEnv = UBootEnv{runner}
	variables, err = fakeEnv.ReadEnv("var")
	assert.NoError(t, err)
	assert.Equal(t, variables["var"], "1")

	runner = stest.NewTestOSCalls("mender_check_saveenv_canary=1\n", 0)
	fakeEnv = UBootEnv{runner}
	err = fakeEnv.WriteEnv(BootVars{"var": "1"})
	assert.Error(t, err)
}

func Test_PermissionDenied(t *testing.T) {
	env := NewEnvironment(new(system.OsCalls))
	vars, err := env.ReadEnv("var")
	assert.Error(t, err)
	assert.Nil(t, vars)

	err = env.WriteEnv(nil)
	assert.Error(t, err)
}

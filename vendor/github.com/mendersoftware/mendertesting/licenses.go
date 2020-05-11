// Copyright 2020 Northern.tech AS
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

package mendertesting

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const packageLocation string = "github.com/mendersoftware/mendertesting"

var known_license_files []string = []string{}

// Specify a license file for a dependency explicitly, avoiding the check for
// common license file names.
func SetLicenseFileForDependency(license_file string) {
	known_license_files = append(known_license_files, "--add-license="+license_file)
}

var firstEnterpriseCommit = ""

// This should be set to the oldest commit that is not part of Open Source, only
// part of Enterprise, if any. IOW it should be the very first commit after the
// fork point, on the Enterprise branch.
func SetFirstEnterpriseCommit(sha string) {
	firstEnterpriseCommit = sha
}

func CheckMenderCompliance(t *testing.T) {
	t.Run("Checking Mender compliance", func(t *testing.T) {
		err := checkMenderCompliance()
		assert.NoError(t, err, err)
	})
}

type MenderComplianceError struct {
	Output string
	Err    error
}

func (m *MenderComplianceError) Error() string {
	return fmt.Sprintf("MenderCompliance failed with error: %s\nOutput: %s\n", m.Err, m.Output)
}

func checkMenderCompliance() error {
	pathToTool, err := locatePackage()
	if err != nil {
		return err
	}

	args := []string{path.Join(pathToTool, "check_license_go_code.sh")}
	if firstEnterpriseCommit != "" {
		args = append(args, "--ent-start-commit", firstEnterpriseCommit)
	}
	cmd := exec.Command("bash", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &MenderComplianceError{
			Err:    err,
			Output: string(output),
		}
	}

	args = []string{path.Join(pathToTool, "check_commits.sh")}
	cmd = exec.Command("bash", args...)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return &MenderComplianceError{
			Err:    err,
			Output: string(output),
		}
	}

	args = []string{path.Join(pathToTool, "check_license.sh")}
	args = append(args, known_license_files...)
	cmd = exec.Command("bash", args...)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return &MenderComplianceError{
			Err:    err,
			Output: string(output),
		}
	}
	return nil
}

func locatePackage() (string, error) {
	finalpath := path.Join("vendor", packageLocation)
	_, err := os.Stat(finalpath)
	if err == nil {
		return finalpath, nil
	}

	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		return "", errors.New("Cannot check for licenses if " +
			"mendertesting is not vendored and GOPATH is unset.")
	}

	paths := strings.Split(gopath, ":")
	for i := 0; i < len(paths); i++ {
		finalpath = path.Join(paths[i], "src", packageLocation)
		_, err := os.Stat(finalpath)
		if err == nil {
			return finalpath, nil
		}
	}

	return "", fmt.Errorf("Package '%s' could not be located anywhere in GOPATH (%s)",
		packageLocation, gopath)
}

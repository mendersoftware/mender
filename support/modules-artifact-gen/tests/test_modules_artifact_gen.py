# Copyright 2023 Northern.tech AS
#
#    Licensed under the Apache License, Version 2.0 (the "License");
#    you may not use this file except in compliance with the License.
#    You may obtain a copy of the License at
#
#        http://www.apache.org/licenses/LICENSE-2.0
#
#    Unless required by applicable law or agreed to in writing, software
#    distributed under the License is distributed on an "AS IS" BASIS,
#    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#    See the License for the specific language governing permissions and
#    limitations under the License.

import os
import subprocess
import tempfile
import shutil
import logging

import pytest

logger = logging.getLogger(__name__)


class TestModulesArtifactGen:
    @pytest.mark.parametrize(
        "tc",
        [
            {"name": "basic case",},
            {"name": "no arguments", "expect_fail": True, "override_args": " ",},
            {
                "name": "non existing",
                "expect_fail": True,
                "override_args": "-n artifact-name -t device-type -d /dest/dir non-existing-file",
            },
            {
                "name": "missing artifact name",
                "expect_fail": True,
                "override_args": "-t device-type -d /dest/dir my-file",
            },
            {
                "name": "missing device type",
                "expect_fail": True,
                "override_args": "-n artifact-name -d /dest/dir my-file",
            },
            {
                "name": "missing dest dir",
                "expect_fail": True,
                "override_args": "-n artifact-name -t device-type my-file",
            },
            {
                "name": "missing file",
                "expect_fail": True,
                "override_args": "-n artifact-name -t device-type -d /dest/dir",
            },
            {
                "name": "add software-name",
                "append_args": " --software-name custom",
                "skip_output_asserts": True,
                "extra_output_asserts": [
                    """Provides:
	rootfs-image.custom.version: artifact-name""",
                ],
            },
            {
                "name": "add software-version",
                "append_args": " --software-version custom",
                "skip_output_asserts": True,
                "extra_output_asserts": [
                    """Provides:
	rootfs-image.single-file.version: custom""",
                ],
            },
            {
                "name": "add software-filesystem",
                "append_args": " --software-filesystem custom",
                "skip_output_asserts": True,
                "extra_output_asserts": [
                    """Provides:
	custom.single-file.version: artifact-name""",
                ],
            },
            {
                "name": "add all software",
                "append_args": " --software-filesystem custom1 --software-name custom2 --software-version custom3",
                "skip_output_asserts": True,
                "extra_output_asserts": [
                    """Provides:
	custom1.custom2.version: custom3""",
                ],
            },
            {
                "name": "extra device type",
                "append_args": " -t other-device-type",
                "extra_output_asserts": [
                    "Compatible devices: '[device-type other-device-type]'",
                ],
            },
            {
                "name": "pass through arguments provides",
                "append_args": " -- --provides some:other --provides thing:else --provides-group group-to-provide",
                "extra_output_asserts": [
                    """Provides:
	rootfs-image.single-file.version: artifact-name
	some: other
	thing: else""",
                    "Provides group: group-to-provide",
                ],
            },
            {
                "name": "pass through arguments depends",
                "append_args": " -- --depends some:other --depends thing:else --depends-groups group-to-depend",
                "extra_output_asserts": [
                    """Depends:
	some: other
	thing: else""",
                    "Depends on one of group(s): [group-to-depend]",
                ],
            },
            {
                "name": "pass through invalid arguments",
                "expect_fail": True,
                "append_args": " -- --invalid-flag",
            },
        ],
    )
    def test_single_file_update_module_gen(self, single_file_artifact_gen_path, tc):
        """Test the single-file update module generator"""

        file_tree = tempfile.mkdtemp()
        try:
            update_file = os.path.join(file_tree, "my-file")
            with open(update_file, "w") as fd:
                fd.write("my-content")
            os.chmod(update_file, 0o664)

            artifact_file = os.path.join(file_tree, "my-artifact.mender")

            # Prepare comand args depending of the Test Case
            cmd_args = " -o %s -n artifact-name -t device-type -d /dest/dir %s" % (
                artifact_file,
                update_file,
            )
            if "append_args" in tc:
                cmd_args += tc["append_args"]
            if "override_args" in tc and tc["override_args"]:
                cmd_args = tc["override_args"]

            # Execute the command
            cmd = single_file_artifact_gen_path + cmd_args
            logger.info("Executing: %s ", cmd)
            if "expect_fail" in tc and tc["expect_fail"]:
                with pytest.raises(subprocess.CalledProcessError):
                    subprocess.check_call(cmd, shell=True)
                return
            else:
                subprocess.check_call(cmd, shell=True)

            # Read back with mender-artifact
            cmd = "mender-artifact read %s" % artifact_file
            logger.info("Executing: %s ", cmd)
            output = subprocess.check_output(cmd, shell=True).decode().strip()

            # Check output
            if not "skip_output_asserts" in tc or not tc["skip_output_asserts"]:
                assert "Name: artifact-name" in output, output
                assert "Compatible devices: '[device-type" in output, output
                assert "Type:   single-file" in output, output
                assert (
                    """Provides:
	rootfs-image.single-file.version: artifact-name"""
                    in output
                ), output
                assert (
                    """Files:
      name:     dest_dir"""
                    in output
                ), output
                assert "name:     filename" in output, output
                assert "name:     permissions" in output, output
                assert "name:     my-file" in output, output
            if "extra_output_asserts" in tc:
                for output_assert in tc["extra_output_asserts"]:
                    assert output_assert in output, output

            # Check file contents
            cmd = "tar -C %s -xf %s data/0000.tar.gz" % (file_tree, artifact_file)
            logger.info("Executing: %s ", cmd)
            subprocess.check_call(cmd, shell=True)
            cmd = "tar -C %s -xzf %s/data/0000.tar.gz" % (file_tree, file_tree)
            logger.info("Executing: %s ", cmd)
            subprocess.check_call(cmd, shell=True)
            with open(os.path.join(file_tree, "dest_dir")) as fd:
                assert "/dest/dir" == fd.read().strip()
            with open(os.path.join(file_tree, "filename")) as fd:
                assert "my-file" == fd.read().strip()
            with open(os.path.join(file_tree, "permissions")) as fd:
                assert "664" == fd.read().strip()
            with open(os.path.join(file_tree, "my-file")) as fd:
                assert "my-content" == fd.read().strip()

        finally:
            shutil.rmtree(file_tree)

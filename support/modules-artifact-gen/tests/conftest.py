# Copyright 2021 Northern.tech AS
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

import pytest

import os
import pathlib
import distutils.spawn

MODULES_ARTIFACT_GEN_PATH = pathlib.Path(__file__).parent.parent.absolute()


@pytest.fixture(scope="session")
def single_file_artifact_gen_path(request):
    return os.path.join(MODULES_ARTIFACT_GEN_PATH, "single-file-artifact-gen")


def pytest_configure(config):
    verify_sane_test_environment()


def verify_sane_test_environment():
    # check if required tools are in PATH, add any other checks here
    if distutils.spawn.find_executable("mender-artifact") is None:
        raise SystemExit("mender-artifact not found in PATH")

# Copyright 2026 Northern.tech AS
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
import pathlib
import shutil
import subprocess

import pytest

MODULES_PATH = pathlib.Path(__file__).parent.parent.absolute()


def run_module(module, state, files_dir):
    return subprocess.run(
        [str(MODULES_PATH / module), state, str(files_dir)],
        capture_output=True,
        text=True,
        check=False,
    )


def snapshot(directory):
    return {
        str(path.relative_to(directory)): path.read_text()
        for path in directory.rglob("*")
        if path.is_file()
    }


@pytest.fixture
def fake_bin(tmp_path, monkeypatch):
    bin_dir = tmp_path / "fake-bin"
    bin_dir.mkdir()
    monkeypatch.setenv("PATH", f"{bin_dir}{os.pathsep}{os.environ['PATH']}")
    return bin_dir


def install_fake(bin_dir, name, body):
    """Shadow `name` on PATH with a script running `body` before the real command.

    Injecting the failure through PATH keeps the outcome identical for root,
    which is exempt from the permission checks that an unwritable backup
    directory would otherwise rely on.
    """
    real = shutil.which(name, path=os.environ["PATH"].split(os.pathsep, 1)[1])
    assert real is not None, f"{name} not found on PATH"
    fake = bin_dir / name
    fake.write_text(f'#!/bin/sh\n{body}\nexec {real} "$@"\n')
    fake.chmod(0o755)


def payload_skeleton(tmp_path, dest_dir):
    files_dir = tmp_path / "payload"
    (files_dir / "files").mkdir(parents=True)
    (files_dir / "tmp").mkdir()
    (files_dir / "files" / "dest_dir").write_text(f"{dest_dir}\n")
    return files_dir


def directory_payload(tmp_path, dest_dir):
    files_dir = payload_skeleton(tmp_path, dest_dir)
    staging = tmp_path / "update-staging"
    staging.mkdir()
    (staging / "fresh.txt").write_text("fresh")
    subprocess.run(
        [
            "tar",
            "-cf",
            str(files_dir / "files" / "update.tar"),
            "-C",
            str(staging),
            ".",
        ],
        check=True,
    )
    return files_dir


def single_file_payload(tmp_path, dest_dir):
    files_dir = payload_skeleton(tmp_path, dest_dir)
    (files_dir / "files" / "filename").write_text("app.conf\n")
    (files_dir / "files" / "app.conf").write_text("fresh")
    return files_dir


# The single-file module only backs up a destination file that already exists,
# so its pre-existing file has to be the one the payload replaces.
MODULES = [
    {
        "name": "directory",
        "payload": directory_payload,
        "before": {"stale.txt": "stale"},
        "after": {"fresh.txt": "fresh"},
        "backup_command": "tar",
        "fail_backup": """
case "$*" in
    *-cf*) echo "tar: simulated backup failure" >&2; exit 3 ;;
esac
""",
    },
    {
        "name": "single-file",
        "payload": single_file_payload,
        "before": {"app.conf": "stale"},
        "after": {"app.conf": "fresh"},
        "backup_command": "cp",
        "fail_backup": """
case "$3" in
    */tmp/backup/*) echo "cp: simulated backup failure" >&2; exit 3 ;;
esac
""",
    },
]

MODULE_CASES = pytest.mark.parametrize(
    "tc", MODULES, ids=[module["name"] for module in MODULES]
)


def populate(tmp_path, tc):
    dest_dir = tmp_path / "dest"
    dest_dir.mkdir()
    for name, content in tc["before"].items():
        (dest_dir / name).write_text(content)
    return dest_dir, tc["payload"](tmp_path, dest_dir)


class TestUpdateModules:
    @MODULE_CASES
    def test_install_applies_the_payload(self, tmp_path, tc):
        dest_dir, files_dir = populate(tmp_path, tc)

        result = run_module(tc["name"], "ArtifactInstall", files_dir)

        assert result.returncode == 0, result.stderr
        assert snapshot(dest_dir) == tc["after"]

    @MODULE_CASES
    def test_install_fails_when_backup_fails(self, tmp_path, fake_bin, tc):
        """A destination that cannot be backed up must fail the deployment.

        In the field this is triggered by the backup running out of space on the
        data partition. The directory module is the most exposed, since its
        backup has to hold a copy of everything currently in the destination
        directory, related to the update or not.
        """
        dest_dir, files_dir = populate(tmp_path, tc)
        install_fake(fake_bin, tc["backup_command"], tc["fail_backup"])

        result = run_module(tc["name"], "ArtifactInstall", files_dir)

        assert "simulated backup failure" in result.stderr, (
            "the fake was bypassed, so this run did not exercise a failing "
            f"backup at all: {result.stderr}"
        )
        assert result.returncode != 0, (
            "ArtifactInstall reported success without applying the update, so "
            "the server records the new software version for a deployment that "
            "never happened"
        )
        assert snapshot(dest_dir) == tc["before"]
        assert snapshot(files_dir / "tmp") == {}, "half-finished backup left behind"

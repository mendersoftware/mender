#!/usr/bin/env python3

import os
import sys
import requests
import json

THIS_DIR = os.path.dirname(os.path.abspath(__file__))

INVALID_KNOWN_FILES = [
    ".gitlab-ci.yml",  # Local file `.gitlab-ci-check-commits.yml` does not have project!
    ".gitlab-ci-template-k8s-test.yml",  # jobs config should contain at least one visible job
]


def lint_file(file):
    """Lints GitLab CI file. Returns True on success"""
    with open(file) as f:
        r = requests.post(
            "https://gitlab.com/api/v4/ci/lint",
            json={"content": f.read()},
            verify=False,
        )

    if r.status_code != requests.codes["OK"]:
        return False

    data = r.json()
    if data["status"] != "valid":
        print("File %s returned the following errors:" % file)
        for error in data["errors"]:
            print(error)
        return False

    return True


def main():

    success = True
    for file in os.listdir(THIS_DIR):
        if file.endswith(".yml") and not file in INVALID_KNOWN_FILES:
            if not lint_file(file):
                success = False

    if not success:
        sys.exit(1)
    sys.exit(0)


if __name__ == "__main__":
    main()

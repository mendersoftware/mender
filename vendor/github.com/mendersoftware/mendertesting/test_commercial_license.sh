#!/bin/bash

# Copyright 2019 Northern.tech AS
#
#    Licensed under the Apache License, Version 2.0 (the "License");
#    you may not use this file except in compliance with the License.
#    You may obtain a copy of the License at
#
#        http:#www.apache.org/licenses/LICENSE-2.0
#
#    Unless required by applicable law or agreed to in writing, software
#    distributed under the License is distributed on an "AS IS" BASIS,
#    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#    See the License for the specific language governing permissions and
#    limitations under the License.

set -e

git_commit() {
    git commit "$@"
    local ret=$?
    # Git has a maximum resolution of one second, and we need --date-order to produce correct output.
    sleep 1
    return $ret
}

do_check() {
    echo "TEST: $1..."
    shift

    if "$(dirname "$0")/check_license_go_code.sh" "$@"; then
        echo PASSED
        return 0
    else
        echo FAILED
        return 1
    fi
}

do_negative_check() {
    echo "TEST: $1..."
    shift

    if "$(dirname "$0")/check_license_go_code.sh" "$@"; then
        echo FAILED
        return 1
    else
        echo PASSED
        return 0
    fi
}

if [ $(ls | wc -l) -ne 0 ]; then
    echo "Run this from an empty temporary directory, please!"
    exit 1
fi

# Create fake git repository with an Open Source and Enterprise history.
git init

# This is needed for Gitlab runners that have no identity. This is a local
# change, so it won't affect the user.
git config user.email "test@test.com"
git config user.name "Test Testison"

# We use this year to compare with Git dates later on. It's possible that this
# will not pass due to race conditions exactly around midnight of 31st of
# December. If you are one of the people debugging this, STOP IT immediately,
# and go out and celebrate the new year instead!
THISYEAR=$(date +%Y)

cat > os-file.go <<EOF
// Copyright $THISYEAR Northern.tech AS
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
EOF
git add os-file.go
git_commit -m 'Initial OS revision'
git checkout -b os

# Go back and make an Enterprise commit forked from the first commit
git checkout master
cat > ent-file.go <<EOF
// Copyright $THISYEAR Northern.tech AS
//
//    All Rights Reserved
EOF
git add ent-file.go
git_commit -m 'Initial ENT revision'
FIRST_ENT_COMMIT=$(git rev-parse HEAD)

git checkout os
echo "code_blah_blah" >> os-file.go
git_commit -a -m 'More OS code'

do_check "Initial check"

# First merge of Open Source into Enterprise.
git checkout master
git merge -m "Merge OS" os

do_check "Check while we're on Enterprise branch" --ent-start-commit $FIRST_ENT_COMMIT
git checkout os
do_check "Check while we're on OS branch"

git checkout os
cp os-file.go os-file2.go
git add os-file2.go
git_commit -m "More OS files"
do_check "Make one more file in each branch"

git checkout master
cp ent-file.go ent-file2.go
git add ent-file2.go
git_commit -m "More ENT files"
do_check "More ENT files" --ent-start-commit $FIRST_ENT_COMMIT
git merge -m "Merge OS" os
do_check "More ENT files after merge" --ent-start-commit $FIRST_ENT_COMMIT

git checkout master
cp os-file.go ent-file3.go
git add ent-file3.go
git_commit -m "Wrong ENT files"
do_negative_check "Test that you cannot use an OS license in Enterprise" --ent-start-commit $FIRST_ENT_COMMIT

cp ent-file.go os-file3.go
git checkout os
git add os-file3.go
git_commit -m "Wrong OS files"
do_negative_check "Test that you cannot use an Enterprise license in OS"

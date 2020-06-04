#!/bin/bash

usage() {
    cat <<EOF
$(basename "$0") [--ent-start-commit=COMMIT]

Checks that all licenses on Go files are correct.

--ent-start-commit=COMMIT
	For an Enterprise repository, specifies the earliest commit that is part
	of only Enterprise (the very first commit after the fork point)
EOF
}

while [ -n "$1" ]; do
    case "$1" in
        --ent-start-commit=*)
            ENT_COMMIT="${1#--ent-start-commit=}"
            ;;
        --ent-start-commit)
            shift
            ENT_COMMIT="$1"
            ;;
        *)
            echo "Unrecognized option $1"
            usage
            exit 1
            ;;
    esac
    shift
done

cat > license-os.tmp <<EOF
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
LINES_OS=$(cat license-os.tmp | wc -l)
# we need to add two extra lines missing from the license preamble
# // Copyright <copyright_year> Northern.tech AS
# //
LINES_OS=$(($LINES_OS + 2))

cat > license-ent.tmp <<EOF
//    All Rights Reserved
EOF
LINES_ENT=$(cat license-ent.tmp | wc -l)
# we need to add two extra lines missing from the license preamble
# // Copyright <copyright_year> Northern.tech AS
# //
LINES_ENT=$(($LINES_ENT + 2))

TEST_RESULT=0
LATEST_OS_COMMIT=

check_file() {
    local file="$1"

    local license
    local lines
    local lic_type
    if is_enterprise "$file"; then
        license="license-ent.tmp"
        lines=$LINES_ENT
        lic_type="Enterprise"
    else
        license="license-os.tmp"
        lines=$LINES_OS
        lic_type="Open Source"
    fi

    modified_year=$(git log --follow -n1 --format=%ad --date=format:%Y -- "$file")

    head -n $lines "$file" | tail -n +3 | diff -qu "$license" - > /dev/null
    if [ ! "$?" -eq "0" ]; then
        echo "!!! FAILED license check on $file. Expected this $lic_type license:"
        cat "$license"
        TEST_RESULT=1
    else
        copyright_modified=$(echo "// Copyright <copyright_year> Northern.tech AS" | sed "s/<copyright_year>/$modified_year/g")
        copyright_file="$(head -n 1 "$file")"
        if [ "$copyright_modified" != "$copyright_file" ]; then
            echo "!!! FAILED license check on $file; make sure copyright year matches last modified year of the file ($modified_year)"
            TEST_RESULT=1
        fi
    fi
}

is_enterprise() {
    local file="$1"

    if [ -z "$ENT_COMMIT" ]; then
        # If there is no Enterprise commit specified, then this isn't an
        # Enterprise repository, so everything is Open Source.
        return 1
    fi

    # Find the latest commit that is not a descendant of the Enterprise
    # commit. This should be the latest Open Source commit. This doesn't change
    # over the course of a run, so cache it.
    if [ -z "$LATEST_OS_COMMIT" ]; then
        LATEST_OS_COMMIT=$(git rev-list $ENT_COMMIT..HEAD --ancestry-path --boundary --date-order | grep "^-" | head -n1 | grep -o '[0-9a-f]*')
    fi

    if git show $LATEST_OS_COMMIT:"$file" >& /dev/null; then
        # File exists, it's Open Source.
        return 1
    else
        # File does not exist, it's Enterprise.
        return 0
    fi
}

for each in $(find . -type f \( ! -regex '.*/\..*' ! -path "./Godeps/*" ! -path "./vendor/*" -name '*.go' \)); do
    check_file "$each"
done

rm -f license-os.tmp license-ent.tmp
exit $TEST_RESULT

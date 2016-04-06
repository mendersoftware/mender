#!/bin/bash
set -x

echo "Checking range: ${TRAVIS_COMMIT_RANGE}"

for i in $( (git rev-list --no-merges "$TRAVIS_COMMIT_RANGE") )
do
    COMMIT_MSG="$(git show -s --format=%B "$i")"
    COMMIT_USER_EMAIL="$(git show -s --format="%an <%ae>" "$i")"
    echo "$COMMIT_MSG" | grep -F "Signed-off-by: ${COMMIT_USER_EMAIL}" >/dev/null

    if [ $? -ne 0 ]; then
        echo >&2 "Commit ${i} is not signed off! Use --signoff with your commit."
        exit 1
    fi

done

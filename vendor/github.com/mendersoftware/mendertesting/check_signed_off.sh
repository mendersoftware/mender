#!/bin/bash

set -e

case "$1" in
    -h|--help)
        echo "usage: $(basename $0) <git-range>"
        exit 1
        ;;
esac

if [ -n "$1" ]
then
    COMMIT_RANGE="$1"
elif [ -n "$TRAVIS_BRANCH" ]
then
    COMMIT_RANGE="origin/$TRAVIS_BRANCH..HEAD"
else
    # Just check previous commit if nothing else is specified.
    COMMIT_RANGE=HEAD~1..HEAD
fi

echo "Checking range: ${COMMIT_RANGE}:"
git log "$COMMIT_RANGE"

commits="$(git rev-list --no-merges "$COMMIT_RANGE")"
notsigned=
for i in $commits
do
    COMMIT_MSG="$(git show -s --format=%B "$i")"
    COMMIT_USER_EMAIL="$(git show -s --format="%an <%ae>" "$i")"

    if ! echo "$COMMIT_MSG" | grep -F "Signed-off-by: ${COMMIT_USER_EMAIL}" >/dev/null; then
        echo >&2 "Commit ${i} is not signed off! Use --signoff with your commit."
        notsigned="$notsigned $i"
    fi

done

if [ -n "$notsigned" ]
then
    exit 1
fi

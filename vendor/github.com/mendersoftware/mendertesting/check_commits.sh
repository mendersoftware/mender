#!/bin/bash

set -e

case "$1" in
    -h|--help)
        echo "usage: $(basename $0) <git-range>"
        exit 1
        ;;
esac

if [ -n "$TRAVIS_BRANCH" ]
then
    COMMIT_RANGE="$TRAVIS_BRANCH..HEAD"
else
    # Just check previous commit if nothing else is specified.
    COMMIT_RANGE=HEAD~1..HEAD
fi

if [ -n "$1" ]
then
    echo "Checking range: $@:"
    git --no-pager log "$@"
    commits="$(git rev-list --no-merges "$@")"
else
    echo "Checking range: ${COMMIT_RANGE}:"
    git --no-pager log "$COMMIT_RANGE"
    commits="$(git rev-list --no-merges "$COMMIT_RANGE")"
fi
notvalid=
for i in $commits
do
    COMMIT_MSG="$(git show -s --format=%B "$i")"
    COMMIT_USER_EMAIL="$(git show -s --format="%an <%ae>" "$i")"

    # Ignore commits that have git-subtree tags in them. They are a PITA both
    # to sign and add changelogs to, and signing should anyway be present in the
    # original repository.
    if echo "$COMMIT_MSG" | egrep "^git-subtree-[^:]+:" >/dev/null; then
        continue
    fi

    # Check that Signed-off-by tags are present.
    if ! echo "$COMMIT_MSG" | grep -F "Signed-off-by: ${COMMIT_USER_EMAIL}" >/dev/null; then
        echo >&2 "Commit ${i} is not signed off! Use --signoff with your commit."
        notvalid="$notvalid $i"
    fi

    # Check that Changelog tags are present.
    if ! echo "$COMMIT_MSG" | grep -i "^ *Changelog:" >/dev/null; then
        echo >&2 "Commit ${i} doesn't have a changelog tag! Make a changelog entry for your commit (https://github.com/mendersoftware/mender/blob/master/CONTRIBUTING.md#changelog-tags)."
        notvalid="$notvalid $i"
    # Less than three words probably means something was misspelled, except for
    # None, Title and Commit.
    elif ! echo "$COMMIT_MSG" | egrep -i "^ *Changelog: *(None|Title|Commit|\S+(\s+\S+){2,}) *$" >/dev/null; then
        echo >&2 "Commit ${i} has less than three words in its changelog tag! Typo? (https://github.com/mendersoftware/mender/blob/master/CONTRIBUTING.md#changelog-tags)."
        notvalid="$notvalid $i"
    fi
done

if [ -n "$notvalid" ]
then
    exit 1
fi

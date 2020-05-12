#!/bin/bash

set -e

while [[ $# -gt 0 ]]
do
    case "$1" in
        -h|--help)
            echo "usage: $(basename $0) [OPTIONS] <git-range>"
            echo
            echo "    --signoffs    Enable checking of signoffs"
            echo "    --changelogs  Enable checking of changelogs"
            echo
            echo "NOTE: In the case that none of the above flags are set"
            echo "      then they are both enabled by default."
            exit 1
            ;;
        -s|--signoffs)
            CHECK_SIGNOFFS=TRUE
            shift
            continue
            ;;
        -c|--changelogs)
            CHECK_CHANGELOGS=TRUE
            shift
            continue
            ;;
        *)
            break
            ;;
    esac
done

# Special case, no Signoff or Changelog flags set -> Do both
if [ -z $CHECK_SIGNOFFS ] && [ -z $CHECK_CHANGELOGS ]; then
    CHECK_SIGNOFFS=TRUE
    CHECK_CHANGELOGS=TRUE
fi

if [ -z "$COMMIT_RANGE" ] && [ -n "$CI_COMMIT_REF_NAME" ]
then
    # Gitlab unfortunately doesn't record base branches of commits when the PR
    # comes from Github, so we need to detect branch names of PRs manually, and
    # then reconstruct the correct range from that, by excluding all other
    # branches.
    case "$CI_COMMIT_REF_NAME" in
        pr_[0-9]*)
            EXCLUDE_LIST=$(mktemp)
            EXCLUDE_LIST_REMOVE=$(mktemp)
            git for-each-ref --format='%(refname)' | sort > $EXCLUDE_LIST
            git for-each-ref --format='%(refname)' --points-at $CI_COMMIT_REF_NAME | sort > $EXCLUDE_LIST_REMOVE
            TO_EXCLUDE="$(comm -23 $EXCLUDE_LIST $EXCLUDE_LIST_REMOVE | tr '\n' ' ')"
            COMMIT_RANGE="$CI_COMMIT_REF_NAME --not $TO_EXCLUDE"
            rm -f $EXCLUDE_LIST $EXCLUDE_LIST_REMOVE
            ;;
    esac
fi

if [ -z "$COMMIT_RANGE" ] && [ -n "$TRAVIS_BRANCH" ]
then
    COMMIT_RANGE="$TRAVIS_BRANCH..HEAD"
fi

if [ -z "$COMMIT_RANGE" ]
then
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
    git --no-pager log $COMMIT_RANGE
    commits="$(git rev-list --no-merges $COMMIT_RANGE)"
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

    if [ ! -z $CHECK_SIGNOFFS ]; then
        # Check that Signed-off-by tags are present.
        if ! echo "$COMMIT_MSG" | grep -F "Signed-off-by: ${COMMIT_USER_EMAIL}" >/dev/null; then
            echo >&2 "Commit ${i} is not signed off! Use --signoff with your commit."
            notvalid="$notvalid $i"
        fi
    fi

    if [ ! -z $CHECK_CHANGELOGS ]; then
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
    fi
done

if [ -n "$notvalid" ]
then
    exit 1
fi

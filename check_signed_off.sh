#!/bin/bash

echo "Checking range: ${TRAVIS_COMMIT_RANGE}"

NEW_COMMITS=( $(git log  --format=format:%H "$TRAVIS_COMMIT_RANGE") )

for i in "${NEW_COMMITS[@]}"
do
    PARENTS=$(git cat-file -p "$i" | grep -c "parent")
    if [ "$PARENTS" -gt 1 ];
     then
        #if the commit is a merge, dont check the commit msg.
        continue
     else
        COMMIT_MSG=$(git show -s --format=%B "$i")
        echo "$COMMIT_MSG" | grep '^Signed-off-by: ' >/dev/null || {
            echo >&2 "Commit ${i} is not signed off! Use --signoff with your commit."
            exit 1
        }
    fi
done

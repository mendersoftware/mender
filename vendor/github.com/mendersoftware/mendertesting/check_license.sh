#!/bin/sh

set -e

CHKSUM_FILE=LIC_FILES_CHKSUM.sha256

while [ -n "$1" ]; do
    case "$1" in
        --add-license=*)
            file="${1#--add-license=}"
            KNOWN_LICENSE_FILES="$KNOWN_LICENSE_FILES $file"
            # The file must exist in LIC_FILES_CHKSUM.sha256
            if ! fgrep -q "$file" $CHKSUM_FILE
            then
                echo "$file does not have a checksum in $CHKSUM_FILE"
                exit 1
            fi
            ;;
        -*)
            echo "Usage: $(basename "$0") <dir-to-check>"
            exit 1
            ;;
    esac
    shift
done

if [ -n "$1" ]
then
    cd "$1"
fi

ret=0

# Known licenses must continue to match.
shasum -a 256 -c $CHKSUM_FILE

# Unlisted licenses not allowed.
for file in $(find . -iname 'LICEN[SC]E' -o -iname 'LICEN[SC]E.*' -o -iname 'COPYING')
do
    file=$(echo $file | sed -e 's,./,,')
    if ! fgrep "$(shasum -a 256 $file)" $CHKSUM_FILE > /dev/null
    then
        echo "$file has missing or wrong entry in $CHKSUM_FILE"
        ret=1
    fi
done

# There must be a license at the top level.
if [ LICENSE* = "LICENSE*" ] && [ COPYING* = "COPYING*" ]
then
    echo "No top level license file."
    ret=1
fi

# There must be a license at the top level of each Go dependency.
# The logic is so that each .go source file must have a license file in the same
# directory, or in a parent directory.
for dep_dir in Godeps/_workspace/src vendor
do
    if [ -d "$dep_dir" ]
    then
        for gofile in $(find "$dep_dir" -name '*.go')
        do
            parent_dir="$(dirname "$gofile")"
            found=0
            while [ "$parent_dir" != "$dep_dir" ]
            do
                # Either we need to find a license file, or the file must be
                # covered by one of the license files specified in
                # KNOWN_LICENSE_FILES.
                if [ $(find "$parent_dir" -maxdepth 1 -iname 'LICEN[SC]E' -o -iname 'LICEN[SC]E.*' -o -iname 'COPYING' | wc -l) -ge 1 ]
                then
                    found=1
                    break
                fi
                if [ -n "$KNOWN_LICENSE_FILES" ]
                then
                    for known_file in $KNOWN_LICENSE_FILES
                    do
                        if [ "$(dirname $known_file)" = "$parent_dir" ]
                        then
                            found=1
                            break 2
                        fi
                    done
                fi
                parent_dir="$(dirname "$parent_dir")"
            done
            if [ $found != 1 ]
            then
                echo "No license file to cover $gofile"
                ret=1
                break
            fi
        done
    fi
done

exit $ret

#!/bin/bash
# Copyright 2022 Northern.tech AS
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

case "$1" in
    menderDownload)
        # The sleep is just to make the code path predictable, so that coverage
        # doesn't fluctuate. The client loop should have spun already by the
        # time we get here.
        sleep 1
        exit 0
        ;;
    moduleDownload|moduleDownloadFailExit|moduleDownloadExitHang)
        count=0
        while name=$(cat stream-next); do
            if [ -z "$name" ]; then
                break
            fi
            cat $name > tmp/module-downloaded-file$count
            count=$(($count+1))
        done
        case "$1" in
            moduleDownloadFailExit)
                exit 1
                ;;
            moduleDownloadExitHang)
                sleep 60
                exit 1
                ;;
        esac
        exit 0
        ;;
    moduleDownloadHang)
        sleep 60
        exit 0
        ;;
    moduleDownloadOnlyOne)
        name=$(cat stream-next)
        cat $name > /dev/null
        exit 0
        ;;
    moduleDownloadTwoEntriesOneFile)
        name=$(cat stream-next)
        cat $name > /dev/null
        cat stream-next > /dev/null
        exit 0
        ;;
    moduleDownloadNoZeroEntry)
        name=$(cat stream-next)
        cat $name > /dev/null
        exit 0
        ;;
    moduleDownloadFailure)
        cat stream-next > /dev/null
        exit 1
        ;;
    moduleDownloadStreamNextShortRead)
        dd if=stream-next of=/dev/null bs=1 count=1
        exit 0
        ;;
    moduleDownloadStreamShortRead)
        name=$(cat stream-next)
        dd if=$name of=/dev/null bs=1 count=1
        exit 0
        ;;
esac

exit 1

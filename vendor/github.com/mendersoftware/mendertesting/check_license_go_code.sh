#!/bin/bash

cat > license.tmp <<EOF
// Copyright 2017 Northern.tech AS
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
lines=$(cat license.tmp | wc -l)

ret=0
for each in $(find . -type f \( ! -regex '.*/\..*' ! -path "./Godeps/*" ! -path "./vendor/*" -name '*.go' \)); do
  echo "Checking $each for correct license header"
  head -n $lines $each | diff -qu license.tmp - > /dev/null
  if [ ! "$?" -eq "0" ]; then
    echo "!!! FAILED license check on $each"
    ret=1
  else
    echo "License check passed on $each"
  fi
done

rm -f license.tmp
exit $ret

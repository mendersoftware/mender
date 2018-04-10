#!/bin/bash

cat > license.tmp <<EOF
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
# we need to add two extra lines missing from the license preamble
# // Copyright <copyright_year> Northern.tech AS
# //
lines=$(($lines + 2))

ret=0
for each in $(find . -type f \( ! -regex '.*/\..*' ! -path "./Godeps/*" ! -path "./vendor/*" -name '*.go' \)); do
  modified_year=$(git log -n1 --format=%ad --date=format:%Y -- $each)
  
  echo "Checking $each for correct license header; last modified in $modified_year"

  head -n $lines $each | tail -n +3 | diff -qu license.tmp - > /dev/null
  if [ ! "$?" -eq "0" ]; then
    echo "!!! FAILED license check on $each"
    ret=1
  else
    copyright_modified=$(echo "// Copyright <copyright_year> Northern.tech AS" | sed "s/<copyright_year>/$modified_year/g")
    copyright_file=$(head -n 1 $each)
    if [ "$copyright_modified" == "$copyright_file" ]; then
      echo "License check passed on $each"
    else
      echo "!!! FAILED license check on $each; make sure copyright year matches last modified year of the file"
      ret=1
    fi
  fi
done

rm -f license.tmp
exit $ret

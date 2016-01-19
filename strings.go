// Copyright 2016 Mender Software AS
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
package main

// Returns a list of pointer to strings, with each of the elements from the
// arguments.
func StringPointerList(content ...string) []*string {
	ret := make([]*string, len(content))
	for i := 0; i < len(content); i++ {
		ret[i] = new(string)
		*ret[i] = content[i]
	}
	return ret
}

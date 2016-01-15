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

const debug_prefix string = "."

type partitionsMockType struct{}

func init() {
	partitions = new(partitionsMockType)
}

func (self *partitionsMockType) getMountedRoot() (string, error) {
	return debug_prefix + "/dev/sda2", nil
}

func (self *partitionsMockType) getActivePartition() (string, error) {
	return debug_prefix + "/dev/sda2", nil
}

func (self *partitionsMockType) getInactivePartition() (string, error) {
	return debug_prefix + "/dev/sda3", nil
}

func (self *partitionsMockType) getActivePartitionNumber() (string, error) {
	return "2", nil
}

func (self *partitionsMockType) getInactivePartitionNumber() (string, error) {
	return "3", nil
}

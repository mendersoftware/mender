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

type InventoryAttribute struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value"`
}

type InventoryData []InventoryAttribute

func (id *InventoryData) ReplaceAttributes(attr []InventoryAttribute) error {
	iMap := make(map[string]InventoryAttribute, len(*id))
	for _, ia := range *id {
		iMap[ia.Name] = ia
	}

	for _, ia := range attr {
		iMap[ia.Name] = ia
	}

	cnt := 0
	for _, v := range iMap {
		if len(*id) > cnt {
			(*id)[cnt] = v
		} else {
			*id = append(*id, v)
		}
		cnt++
	}
	return nil
}

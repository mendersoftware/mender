// Copyright 2021 Northern.tech AS
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

package utils

import (
	"reflect"

	"github.com/pkg/errors"
)

var (
	ErrNotSlice    = errors.New("value is not a slice")
	ErrInvalidType = errors.New("invalid type")
)

// ElemInSlice is a generic function for comparing a value with all elements
// in a slice.
func ElemInSlice(slice, elem interface{}) (bool, error) {
	rSlice := reflect.ValueOf(slice)
	rElem := reflect.ValueOf(elem)
	// Maybe dereference arguments
	if rSlice.Kind() == reflect.Ptr || rSlice.Kind() == reflect.Interface {
		rSlice = rSlice.Elem()
	}
	if rElem.Kind() == reflect.Ptr || rElem.Kind() == reflect.Interface {
		rElem = rElem.Elem()
	}
	if rSlice.Kind() != reflect.Slice {
		return false, ErrNotSlice
	}
	n := rSlice.Len()
	for i := 0; i < n; i++ {
		sliceElem := rSlice.Index(i)
		if sliceElem.Kind() == reflect.Ptr {
			sliceElem = sliceElem.Elem()
		}
		if sliceElem.Kind() == reflect.Interface {
			sliceElem = sliceElem.Elem()
		}
		if sliceElem.Kind() != rElem.Kind() {
			return false, ErrInvalidType
		}
		if sliceElem.Interface() == rElem.Interface() {
			return true, nil
		}
	}
	return false, nil
}

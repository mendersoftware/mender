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

package scopestack

import "fmt"
import "container/list"
import "runtime"

// A stack type that tries to verify that each push and pop happens in the same
// function.
type ScopeStack struct {
	// The list of stack frames where Push() has been called.
	stack list.List
	// The depth at which the scope stack is. If you wrap ScopeStack in a
	// different scoped object, add one to this so that the stack frame of
	// the calling function of that object is considered. Otherwise the
	// stack frame of the scoped object is checked which will always be
	// different. If ScopeStack is used directly, this should be zero.
	scopeDistance int
}

type scopeElement struct {
	frame *uintptr
	value interface{}
}

func NewScopeStack(scopeDistance int) *ScopeStack {
	var ret ScopeStack
	ret.scopeDistance = scopeDistance
	return &ret
}

func (self *ScopeStack) Push(v interface{}) {
	var frame *uintptr
	pc, _, _, ok := runtime.Caller(self.scopeDistance + 1)
	if !ok {
		frame = nil
	} else {
		frame = new(uintptr)
		*frame = pc
	}

	self.stack.PushBack(scopeElement{frame, v})
}

func (self *ScopeStack) Pop() interface{} {
	var oldFrame *uintptr
	var newFrame uintptr

	lElement := self.stack.Back()
	if lElement == nil {
		panic("ScopeStack: Nothing to Pop()")
	}
	sElement := lElement.Value.(scopeElement)
	value := sElement.value
	oldFrame = sElement.frame
	self.stack.Remove(lElement)
	if oldFrame == nil {
		// We cannot perform any checks in this case.
		return value
	}

	pc, _, _, ok := runtime.Caller(self.scopeDistance + 1)
	if !ok {
		// We cannot perform any checks in this case.
		return value
	}
	newFrame = pc

	oldFunc := runtime.FuncForPC(*oldFrame)
	newFunc := runtime.FuncForPC(newFrame)

	if oldFunc.Entry() != newFunc.Entry() {
		oldFile, oldLine := oldFunc.FileLine(*oldFrame)
		newFile, newLine := newFunc.FileLine(newFrame)
		msg := fmt.Sprintf("Unbalanced ScopeStack.Pop(). "+
			"Push inside %s() at %s:%d does not balance "+
			"with pop in %s() at %s:%d "+
			"(Push and Pop have to be in the same function)",
			oldFunc.Name(), oldFile, oldLine,
			newFunc.Name(), newFile, newLine)
		panic(msg)
	}

	return value
}

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

package scopestack

import mt "github.com/mendersoftware/mender/internal/mendertesting"
import "testing"

func TestPushPop(t *testing.T) {
	var s ScopeStack
	s.Push("1")
	mt.AssertTrue(t, s.Pop().(string) == "1")
}

func TestTooMuchPopping(t *testing.T) {
	var s ScopeStack
	s.Push("1")
	mt.AssertTrue(t, s.Pop().(string) == "1")
	defer func() {
		if recover() == nil {
			t.Fatal("Unbalanced Pop() did not panic.")
		}
	}()
	mt.AssertTrue(t, s.Pop().(string) == "should never happen")
	t.Fatal("Should never get here")
}

func TestPopInDefer(t *testing.T) {
	var s ScopeStack
	defer s.Pop()
	s.Push("1")
}

func TestPushPopNotInSameFunction(t *testing.T) {
	var s ScopeStack
	func() {
		s.Push("1")
	}()
	defer func() {
		if recover() == nil {
			t.Fatal("Pop() should have panicked when used in a " +
				"different function than Push()")
		}
	}()
	s.Pop()
	t.Fatal("Should never get here")
}

func pushScopeStackDirectly(s *ScopeStack) {
	s.Push("1")
}

func pushScopeStackIndirectly(s *ScopeStack) {
	pushScopeStackDirectly(s)
}

func popScopeStackDirectly(s *ScopeStack) {
	s.Pop()
}

func popScopeStackIndirectly(s *ScopeStack) {
	popScopeStackDirectly(s)
}

func TestDifferentScopeDistance(t *testing.T) {
	var s *ScopeStack = NewScopeStack(1)

	pushScopeStackDirectly(s)
	popScopeStackDirectly(s)

	func() {
		// With scope distance 1, it should not reach all the way out
		// to this function, and should therefore fail because Push()
		// and Pop() are in different functions.
		pushScopeStackIndirectly(s)
		defer func() {
			if recover() == nil {
				t.Fatal("Should have panicked because " +
					"scope stack distance should point " +
					"to this function")
			}
		}()
		popScopeStackIndirectly(s)
		t.Fatal("Should never get here")
	}()

	// Now change the scope distance to 2. Now it should reach all the way
	// out to this test function.
	s = NewScopeStack(2)

	pushScopeStackIndirectly(s)
	popScopeStackIndirectly(s)
}

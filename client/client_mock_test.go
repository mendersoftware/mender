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
package client

import (
	"net/http"

	"github.com/stretchr/testify/mock"
)

type mockApiClient struct {
	mock.Mock
}

func NewMockApiClient(rsp *http.Response, err error) *mockApiClient {
	m := &mockApiClient{}
	m.On("Do", mock.AnythingOfType("*http.Request")).Return(rsp, err)
	return m
}

func (m *mockApiClient) Do(req *http.Request) (*http.Response, error) {
	ret := m.Called(req)
	return ret.Get(0).(*http.Response), ret.Error(1)
}

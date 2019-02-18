// Copyright 2018 Northern.tech AS
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
package datastore

// Gather all datastore keys in this file so that there is an index over what
// keys exist. Since these are stored in the client database they may be long
// lived and require special care during migrations and such.
//
// Add any usage data / compatibility notes / historical data about each
// database key here.

const (
	// Name of artifact currently installed. Introduced in Mender 2.0.0.
	ArtifactNameKey = "artifact-name"

	// Key used to store the auth token.
	AuthTokenName = "authtoken"

	// The key used by the standalone installer to track artifacts that have
	// been started, but not committed. We don't want to use the
	// StateDataKey for this, because it contains a lot less information.
	StandaloneStateKey = "standalone-state"

	// Name of key that state data is stored under across reboots. Uses the
	// StateData structure, marshalled to JSON.
	StateDataKey = "state"
)

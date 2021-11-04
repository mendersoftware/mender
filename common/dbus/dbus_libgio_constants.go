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

package dbus

// Enumeration for well-known message buses.
const (
	GBusTypeSystem  = 1
	GBusTypeSession = 2
)

// Enumeration for GBusNameOwnerFlags
const (
	DBusNameOwnerFlagsNone             = 0
	DBusNameOwnerFlagsAllowReplacement = (1 << 0)
	DBusNameOwnerFlagsReplace          = (1 << 1)
	DBusNameOwnerFlagsDoNotQueue       = (1 << 2)
)

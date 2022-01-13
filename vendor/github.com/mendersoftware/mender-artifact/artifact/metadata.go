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

package artifact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/pkg/errors"
)

// WriteValidator is the interface that wraps the io.Writer interface and
// Validate method.
type WriteValidator interface {
	io.Writer
	Validate() error
}

// ErrValidatingData is an error returned by Validate() in case of
// invalid data.
var ErrValidatingData = errors.New("error validating data")

// Info contains the information about the format and the version
// of artifact archive.
type Info struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
}

// Validate performs sanity checks on artifact info.
func (i Info) Validate() error {
	if len(i.Format) == 0 || i.Version == 0 {
		return errors.Wrap(ErrValidatingData, "Artifact Info needs a format type and a version")
	}
	return nil
}

func decode(p []byte, data WriteValidator) error {
	if len(p) == 0 {
		return nil
	}

	dec := json.NewDecoder(bytes.NewReader(p))
	dec.DisallowUnknownFields()
	err := dec.Decode(data)
	if err != nil {
		return err
	}
	return nil
}

func (i *Info) Write(p []byte) (n int, err error) {
	if err := decode(p, i); err != nil {
		return 0, err
	}
	return len(p), nil
}

// UpdateType provides information about the type of update.
// At the moment the only built-in type is "rootfs-image".
type UpdateType struct {
	Type string `json:"type"`
}

// HeaderInfoer wraps headerInfo version 2 and 3,
// in order to supply the artifact reader with the information it needs.
type HeaderInfoer interface {
	Write(b []byte) (n int, err error)
	GetArtifactName() string
	GetCompatibleDevices() []string
	GetUpdates() []UpdateType
	GetArtifactDepends() *ArtifactDepends
	GetArtifactProvides() *ArtifactProvides
}

// HeaderInfo contains information of number and type of update files
// archived in Mender metadata archive.
type HeaderInfo struct {
	ArtifactName      string       `json:"artifact_name"`
	Updates           []UpdateType `json:"updates"`
	CompatibleDevices []string     `json:"device_types_compatible"`
}

func (h *HeaderInfo) UnmarshalJSON(b []byte) error {
	type Alias HeaderInfo
	buf := &Alias{}
	if err := json.Unmarshal(b, &buf); err != nil {
		return err
	}
	if len(buf.CompatibleDevices) == 0 {
		return ErrCompatibleDevices
	}
	h.ArtifactName = buf.ArtifactName
	h.Updates = buf.Updates
	h.CompatibleDevices = buf.CompatibleDevices
	return nil
}

func NewHeaderInfo(
	artifactName string,
	updates []UpdateType,
	compatibleDevices []string,
) *HeaderInfo {
	return &HeaderInfo{
		ArtifactName:      artifactName,
		Updates:           updates,
		CompatibleDevices: compatibleDevices,
	}
}

// Satisfy HeaderInfoer interface for the artifact reader.
func (hi *HeaderInfo) GetArtifactName() string {
	return hi.ArtifactName
}

// Satisfy HeaderInfoer interface for the artifact reader.
func (hi *HeaderInfo) GetCompatibleDevices() []string {
	return hi.CompatibleDevices
}

// Satisfy HeaderInfoer interface for the artifact reader.
func (hi *HeaderInfo) GetUpdates() []UpdateType {
	return hi.Updates
}

// Validate checks if header-info structure is correct.
func (hi HeaderInfo) Validate() error {
	missingArgs := []string{"Artifact validation failed with missing argument"}
	if len(hi.Updates) == 0 {
		missingArgs = append(missingArgs, "No Payloads added")
	}
	if len(hi.CompatibleDevices) == 0 {
		missingArgs = append(missingArgs, "No compatible devices listed")
	}
	if len(hi.ArtifactName) == 0 {
		missingArgs = append(missingArgs, "No artifact name")
	}
	for _, update := range hi.Updates {
		if update == (UpdateType{}) {
			missingArgs = append(missingArgs, "Empty Payload")
			break
		}
	}
	if len(missingArgs) > 1 {
		if len(missingArgs) > 2 {
			missingArgs[0] = missingArgs[0] + "s" // Add plural.
		}
		missingArgs[0] = missingArgs[0] + ": "
		return errors.New(missingArgs[0] + strings.Join(missingArgs[1:], ", "))
	}
	return nil
}

func (hi *HeaderInfo) Write(p []byte) (n int, err error) {
	if err := decode(p, hi); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (hi *HeaderInfo) GetArtifactDepends() *ArtifactDepends {
	return nil
}

func (hi *HeaderInfo) GetArtifactProvides() *ArtifactProvides {
	return nil
}

type HeaderInfoV3 struct {
	// For historical reasons, "payloads" are often referred to as "updates"
	// in the code, since this was the old name (and still is, in V2).
	// This is the reason why the struct field is still called
	// "Updates".
	Updates []UpdateType `json:"payloads"`
	// Has its own json marshaller tags.
	ArtifactProvides *ArtifactProvides `json:"artifact_provides"`
	// Has its own json marshaller tags.
	ArtifactDepends *ArtifactDepends `json:"artifact_depends"`
}

func NewHeaderInfoV3(updates []UpdateType,
	artifactProvides *ArtifactProvides, artifactDepends *ArtifactDepends) *HeaderInfoV3 {
	return &HeaderInfoV3{
		Updates:          updates,
		ArtifactProvides: artifactProvides,
		ArtifactDepends:  artifactDepends,
	}
}

// Satisfy HeaderInfoer interface for the artifact reader.
func (hi *HeaderInfoV3) GetArtifactName() string {
	if hi.ArtifactProvides == nil {
		return ""
	}
	return hi.ArtifactProvides.ArtifactName
}

// Satisfy HeaderInfoer interface for the artifact reader.
func (hi *HeaderInfoV3) GetCompatibleDevices() []string {
	if hi.ArtifactDepends == nil {
		return nil
	}
	return hi.ArtifactDepends.CompatibleDevices
}

// Satisfy HeaderInfoer interface for the artifact reader.
func (hi *HeaderInfoV3) GetUpdates() []UpdateType {
	return hi.Updates
}

// Validate validates the correctness of the header version3.
func (hi *HeaderInfoV3) Validate() error {
	missingArgs := []string{"Artifact validation failed with missing argument"}
	// Artifact must have an update with them,
	// because the signature of the update is stored in the metadata field.
	if len(hi.Updates) == 0 {
		missingArgs = append(missingArgs, "No Payloads added")
	}
	// Updates cannot be empty.
	for _, update := range hi.Updates {
		if update == (UpdateType{}) {
			missingArgs = append(missingArgs, "Empty Payload")
			break
		}
	}
	//////////////////////////////////
	// All required Artifact-provides:
	//////////////////////////////////
	/* Artifact-provides cannot be empty. */
	if hi.ArtifactProvides == nil {
		missingArgs = append(missingArgs, "Empty Artifact provides")
	} else {
		/* Artifact must have a name. */
		if len(hi.ArtifactProvides.ArtifactName) == 0 {
			missingArgs = append(missingArgs, "Artifact name")
		}
		//
		/* Artifact need not have a group */
		//
	}
	///////////////////////////////////////
	// Artifact-depends can be empty, thus:
	///////////////////////////////////////
	/* Artifact must not depend on a name. */
	/* Artifact must not depend on a device. */
	/* Artifact must not depend on an device group. */
	/* Artifact must not depend on a update types supported. */
	/* Artifact must not depend on artifact versions supported. */
	if len(missingArgs) > 1 {
		if len(missingArgs) > 2 {
			missingArgs[0] = missingArgs[0] + "s" // Add plural.
		}
		missingArgs[0] = missingArgs[0] + ": "
		return errors.New(missingArgs[0] + strings.Join(missingArgs[1:], ", "))
	}
	return nil
}

func (hi *HeaderInfoV3) Write(p []byte) (n int, err error) {
	if err := decode(p, hi); err != nil {
		return 0, err
	}
	return len(p), nil
}

type ArtifactDepends struct {
	ArtifactName      []string `json:"artifact_name,omitempty"`
	CompatibleDevices []string `json:"device_type,omitempty"`
	ArtifactGroup     []string `json:"artifact_group,omitempty"`
}

var ErrCompatibleDevices error = errors.New(
	"ArtifactDepends: Required field 'CompatibleDevices' not found",
)

func (a *ArtifactDepends) UnmarshalJSON(b []byte) error {
	type Alias ArtifactDepends // Same fields, no inherited UnmarshalJSON method
	buf := &Alias{}
	if err := json.Unmarshal(b, buf); err != nil {
		return err
	}
	if len(buf.CompatibleDevices) == 0 {
		return ErrCompatibleDevices
	}
	a.ArtifactName = buf.ArtifactName
	a.CompatibleDevices = buf.CompatibleDevices
	a.ArtifactGroup = buf.ArtifactGroup
	return nil
}

type ArtifactProvides struct {
	ArtifactName  string `json:"artifact_name"`
	ArtifactGroup string `json:"artifact_group,omitempty"`
}

// TypeInfo provides information of type of individual updates
// archived in artifacts archive.
type TypeInfo struct {
	Type string `json:"type"`
}

// Validate validates corectness of TypeInfo.
func (ti TypeInfo) Validate() error {
	if len(ti.Type) == 0 {
		return errors.Wrap(ErrValidatingData, "TypeInfo requires a type")
	}
	return nil
}

func (ti *TypeInfo) Write(p []byte) (n int, err error) {
	if err := decode(p, ti); err != nil {
		return 0, err
	}
	return len(p), nil
}

type TypeInfoDepends map[string]interface{}

func (t TypeInfoDepends) Map() map[string]interface{} {
	return map[string]interface{}(t)
}

func NewTypeInfoDepends(m interface{}) (ti TypeInfoDepends, err error) {

	const errMsgInvalidTypeFmt = "Invalid TypeInfo depends type: %T"
	const errMsgInvalidTypeEntFmt = errMsgInvalidTypeFmt + ", with value %v"

	ti = make(map[string]interface{})
	switch val := m.(type) {
	case map[string]interface{}:
		for k, v := range val {
			switch val := v.(type) {

			case string, []string:
				ti[k] = v

			case []interface{}:
				valStr := make([]string, len(val))
				for i, entFace := range v.([]interface{}) {
					entStr, ok := entFace.(string)
					if !ok {
						return nil, fmt.Errorf(
							errMsgInvalidTypeEntFmt,
							v, v)
					}
					valStr[i] = entStr
				}
				ti[k] = valStr

			default:
				return nil, fmt.Errorf(
					errMsgInvalidTypeEntFmt,
					v, v)
			}
		}
		return ti, nil
	case map[string]string:
		m := m.(map[string]string)
		for k, v := range m {
			ti[k] = v
		}
		return ti, nil
	case map[string][]string:
		m := m.(map[string][]string)
		for k, v := range m {
			ti[k] = v
		}
		return ti, nil
	default:
		return nil, fmt.Errorf(errMsgInvalidTypeFmt, m)
	}
}

// UnmarshalJSON attempts to deserialize the json stream into a 'map[string]interface{}',
// where each interface value is required to be either a string, or an array of strings
func (t *TypeInfoDepends) UnmarshalJSON(b []byte) error {
	m := make(map[string]interface{})
	err := json.Unmarshal(b, &m)
	if err != nil {
		return err
	}
	*t, err = NewTypeInfoDepends(m)
	return err
}

type TypeInfoProvides map[string]string

func (t TypeInfoProvides) Map() map[string]string {
	return t
}

func NewTypeInfoProvides(m interface{}) (ti TypeInfoProvides, err error) {

	const errMsgInvalidTypeFmt = "Invalid TypeInfo provides type: %T"
	const errMsgInvalidTypeEntFmt = errMsgInvalidTypeFmt + ", with value %v"

	ti = make(map[string]string)
	switch val := m.(type) {
	case map[string]interface{}:
		for k, v := range val {
			switch val := v.(type) {
			case string:
				ti[k] = val
				continue
			default:
				return nil, fmt.Errorf(errMsgInvalidTypeEntFmt,
					v, v)
			}
		}
		return ti, nil
	case map[string]string:
		m := m.(map[string]string)
		for k, v := range m {
			ti[k] = v
		}
		return ti, nil
	default:
		return nil, fmt.Errorf(errMsgInvalidTypeFmt, m)
	}
}

// UnmarshalJSON attempts to deserialize the json stream into a 'map[string]interface{}',
// where each interface value is required to be either a string, or an array of strings
func (t *TypeInfoProvides) UnmarshalJSON(b []byte) error {
	m := make(map[string]interface{})
	err := json.Unmarshal(b, &m)
	if err != nil {
		return err
	}
	*t, err = NewTypeInfoProvides(m)
	return err
}

// TypeInfoV3 provides information about the type of update contained within the
// headerstructure.
type TypeInfoV3 struct {
	// Rootfs/Delta (Required).
	Type string `json:"type"`

	ArtifactDepends        TypeInfoDepends  `json:"artifact_depends,omitempty"`
	ArtifactProvides       TypeInfoProvides `json:"artifact_provides,omitempty"`
	ClearsArtifactProvides []string         `json:"clears_artifact_provides,omitempty"`
}

// Validate checks that the required `Type` field is set.
func (ti *TypeInfoV3) Validate() error {
	if ti.Type == "" {
		return errors.Wrap(ErrValidatingData, "TypeInfoV3: ")
	}
	return nil
}

// Write writes the underlying struct into a json data structure (bytestream).
func (ti *TypeInfoV3) Write(b []byte) (n int, err error) {
	if err := decode(b, ti); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (hi *HeaderInfoV3) GetArtifactDepends() *ArtifactDepends {
	return hi.ArtifactDepends
}

func (hi *HeaderInfoV3) GetArtifactProvides() *ArtifactProvides {
	return hi.ArtifactProvides
}

// Metadata contains artifacts metadata information. The exact metadata fields
// are user-defined and are not specified. The only requirement is that those
// must be stored in a for of JSON.
// The fields which must exist are update-type dependent. In case of
// `rootfs-update` image type, there are no additional fields required.
type Metadata map[string]interface{}

// Validate check corecness of artifacts metadata. Since the exact format is
// not specified validation always succeeds.
func (m Metadata) Validate() error {
	return nil
}

func (m *Metadata) Write(p []byte) (n int, err error) {
	if err := decode(p, m); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (m *Metadata) Map() map[string]interface{} {
	return map[string]interface{}(*m)
}

// Files represents the list of file names that make up the payload for given
// update.
type Files struct {
	FileList []string `json:"files"`
}

// Validate checks format of Files.
func (f Files) Validate() error {
	for _, f := range f.FileList {
		if len(f) == 0 {
			return errors.Wrap(ErrValidatingData, "File in FileList requires a name")
		}
	}
	return nil
}

func (f *Files) Write(p []byte) (n int, err error) {
	if err := decode(p, f); err != nil {
		return 0, err
	}
	return len(p), nil
}

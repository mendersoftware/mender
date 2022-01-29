// Copyright 2022 Northern.tech AS
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
package test

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/mendersoftware/mender/app/updatecontrolmap"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/datastore"
)

type updateType struct {
	Has          bool
	Data         datastore.UpdateInfo
	Unauthorized bool
	Called       bool
	Current      *client.CurrentUpdate
	ControlMap   *updatecontrolmap.UpdateControlMap
}

type updateDownloadType struct {
	Called bool
	Data   bytes.Buffer
}

type authType struct {
	Authorize bool
	Token     []byte
	Called    bool
	Verify    bool
}

type statusType struct {
	Status  string
	Aborted bool
	Called  bool
}

type logType struct {
	Called bool
	Logs   []byte
}

type inventoryType struct {
	Called bool
	Attrs  []client.InventoryAttribute
}

type requestHeader struct {
	Header http.Header
}

type responseHeader struct {
	Header http.Header
}

type ClientTestServer struct {
	*httptest.Server

	Enterprise     bool
	Update         updateType
	UpdateDownload updateDownloadType
	Auth           authType
	Status         statusType
	Log            logType
	Inventory      inventoryType
	RequestHeader  requestHeader
	ResponseHeader responseHeader
}

// Can be several different types, see switch statement inside
// NewClientTestServer().
type Options interface{}

func NewClientTestServer(options ...Options) *ClientTestServer {
	var tlsConfig *tls.Config
	var mux *http.ServeMux
	for _, opt := range options {
		// Accept several types of arguments that can customize the test server.
		switch o := opt.(type) {
		case *tls.Config:
			tlsConfig = o
		case *http.ServeMux:
			mux = o
		default:
			panic(fmt.Sprintf(
				"Unsupported argument type to NewClientTestServer(): %T", opt))
		}
	}

	cts := &ClientTestServer{}

	if mux == nil {
		mux = http.NewServeMux()
		mux.HandleFunc("/api/devices/v1/authentication/auth_requests", cts.headersHook(cts.authReq))
		mux.HandleFunc(
			"/api/devices/v1/inventory/device/attributes",
			cts.headersHook(cts.inventoryReq),
		)
		mux.HandleFunc(
			"/api/devices/v1/deployments/device/deployments/next",
			cts.headersHook(cts.updateReqv1),
		)
		mux.HandleFunc(
			"/api/devices/v2/deployments/device/deployments/next",
			cts.headersHook(cts.updateReqv2),
		)
		mux.HandleFunc(
			"/api/devices/v1/deployments/device/deployments/",
			cts.headersHook(cts.deploymentsReq),
		)
		mux.HandleFunc("/api/devices/v1/download", cts.headersHook(cts.updateDownloadReq))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			log.Infof("fallback request handler, request %v", r)
			w.WriteHeader(http.StatusBadRequest)
		})
	}

	cts.Server = httptest.NewUnstartedServer(mux)

	if tlsConfig != nil {
		cts.Server.TLS = tlsConfig
		cts.Server.StartTLS()
	} else {
		cts.Server.Start()
	}
	return cts
}

func writeJSON(out io.Writer, data interface{}) error {
	enc := json.NewEncoder(out)
	return enc.Encode(data)
}

func fromJSON(in io.Reader, data interface{}) error {
	dec := json.NewDecoder(in)
	return dec.Decode(data)
}

func (cts *ClientTestServer) Reset() {
	cts.Update = updateType{}
	cts.UpdateDownload = updateDownloadType{}
	cts.Auth = authType{}
	cts.Log = logType{}
	cts.Inventory = inventoryType{}
	cts.Status = statusType{}
	cts.RequestHeader = requestHeader{}
	cts.ResponseHeader = responseHeader{}

}

func isMethod(method string, w http.ResponseWriter, r *http.Request) bool {
	if r.Method != method {
		log.Errorf("method verification failed, expected %v got %v",
			method, r.Method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func isContentType(ct string, w http.ResponseWriter, r *http.Request) bool {
	rct := r.Header.Get("Content-Type")
	if ct != rct {
		log.Errorf("content-type verification failed, expected %v got %v",
			ct, rct)
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return false
	}
	return true
}

// verifyAuth checks that client is authorized and returns false if not.
// ClientTestServer.Auth.Verify must be true for verification to take place.
// Client token must match ClientTestServer.Auth.Token.
func (cts *ClientTestServer) verifyAuth(w http.ResponseWriter, r *http.Request) bool {
	if cts.Auth.Verify {
		hv := r.Header.Get("Authorization")
		if hv == "" {
			log.Errorf("no authorization header")
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		if !strings.HasPrefix(hv, "Bearer ") {
			log.Errorf("bad authorization value: %v", hv)
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}

		s := strings.SplitN(hv, " ", 2)
		tok := s[1]

		if !bytes.Equal(cts.Auth.Token, []byte(tok)) {
			log.Errorf("bad token, got %s expected %s", hv, cts.Auth.Token)
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
	}
	return true
}

func (cts *ClientTestServer) authReq(w http.ResponseWriter, r *http.Request) {
	log.Infof("got auth request %v", r)
	cts.Auth.Called = true

	if !isMethod(http.MethodPost, w, r) {
		return
	}

	if !isContentType("application/json", w, r) {
		return
	}

	if cts.Auth.Authorize {
		w.WriteHeader(http.StatusOK)
		if cts.Auth.Token != nil {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write(cts.Auth.Token)
		}
	} else {
		w.WriteHeader(http.StatusUnauthorized)
	}

}

func (cts *ClientTestServer) headersHook(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for hdr := range cts.RequestHeader.Header {
			h := r.Header.Get(hdr)
			if h == "" {
				log.Errorf("header %s not found, got %+v, expected %+v",
					hdr, r.Header, cts.RequestHeader.Header)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if h != cts.RequestHeader.Header.Get(hdr) {
				log.Errorf("header %s mismatch, got %+v, expected %+v",
					hdr, r.Header, cts.RequestHeader.Header)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		for hdr := range cts.ResponseHeader.Header {
			w.Header().Add(hdr, cts.ResponseHeader.Header.Get(hdr))
		}
		f(w, r)
	}
}

func (cts *ClientTestServer) inventoryReq(w http.ResponseWriter, r *http.Request) {
	log.Infof("got inventory request %v", r)
	cts.Inventory.Called = true

	if !isMethod(http.MethodPut, w, r) {
		return
	}

	if !isContentType("application/json", w, r) {
		return
	}

	if !cts.verifyAuth(w, r) {
		return
	}

	var attrs []client.InventoryAttribute

	if err := fromJSON(r.Body, &attrs); err != nil {
		log.Errorf("failed to parse attrs data: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	log.Infof("got attrs: %v", attrs)
	cts.Inventory.Attrs = attrs
	w.WriteHeader(http.StatusOK)
}

func (cts *ClientTestServer) deploymentsReq(w http.ResponseWriter, r *http.Request) {
	log.Infof("got deployments log/status request %v", r)
	p := r.URL.Path
	s := strings.TrimPrefix(p, "/api/devices/v1/deployments/device/deployments/")
	if s == p {
		// unchanged, was no prefix?
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	log.Infof("request for %v", s)

	idwhat := strings.SplitN(s, "/", 2)
	id := idwhat[0]
	what := idwhat[1]

	switch {
	case what == "log":
		cts.logReq(w, r, id)
	case what == "status":
		cts.statusReq(w, r, id)
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func (cts *ClientTestServer) logReq(w http.ResponseWriter, r *http.Request, id string) {
	log.Infof("got log request deployment ID: %v, %v", id, r)
	cts.Log.Called = true

	if !isMethod(http.MethodPut, w, r) {
		return
	}

	if !isContentType("application/json", w, r) {
		return
	}

	if !cts.verifyAuth(w, r) {
		return
	}

	logs, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Errorf("error when receiving logs: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	log.Infof("got logs: %v", logs)
	cts.Log.Logs = logs
	w.WriteHeader(http.StatusNoContent)
}

func (cts *ClientTestServer) statusReq(w http.ResponseWriter, r *http.Request, id string) {
	log.Infof("got status request deployment ID: %v, %v", id, r)
	cts.Status.Called = true

	if !isMethod(http.MethodPut, w, r) {
		return
	}

	if !isContentType("application/json", w, r) {
		return
	}

	if !cts.verifyAuth(w, r) {
		return
	}

	if cts.Status.Aborted {
		w.WriteHeader(http.StatusConflict)
		return
	}

	var report client.StatusReport
	if err := fromJSON(r.Body, &report); err != nil {
		log.Errorf("failed to parse status data: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	cts.Status.Status = report.Status

	w.WriteHeader(http.StatusNoContent)
}

func urlQueryToCurrentUpdate(vals url.Values) client.CurrentUpdate {
	cur := client.CurrentUpdate{
		Artifact:   vals.Get("artifact_name"),
		DeviceType: vals.Get("device_type"),
	}
	return cur
}

func (cts *ClientTestServer) updateReqv1(w http.ResponseWriter, r *http.Request) {
	var current client.CurrentUpdate
	log.Infof("got update requestv1 %v", r)
	cts.Update.Called = true

	// Enterprise client device provides post is not supported yet
	if r.Method == "POST" {
		if !cts.verifyAuth(w, r) {
			log.Info("Not authorized")
			return
		}
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(500)
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		err = json.Unmarshal(body, &current)
		if err != nil {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(err.Error()))
			return
		}

		if current.Artifact == "" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte("artifact_name missing from payload"))
			return
		}
		if current.DeviceType == "" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte("device_type missing from payload"))
			return
		}
		if !reflect.DeepEqual(current, *cts.Update.Current) {
			log.Errorf("incorrect current update info, got %+v, expected %+v",
				current, *cts.Update.Current)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

	} else if !isMethod(http.MethodGet, w, r) {
		return
	} else {
		if !cts.verifyAuth(w, r) {
			return
		}
		log.Infof("Valid update request GET: %v", r)
		log.Infof("parsed URL query: %v", r.URL.Query())
		if current := urlQueryToCurrentUpdate(r.URL.Query()); cts.Update.Current != nil &&
			!reflect.DeepEqual(current, *cts.Update.Current) {
			log.Errorf("incorrect current update info, got %+v, expected %+v",
				current, *cts.Update.Current)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

	}

	switch {
	case cts.Update.Unauthorized:
		w.WriteHeader(http.StatusUnauthorized)
	case !cts.Update.Has:
		w.WriteHeader(http.StatusNoContent)
	case cts.Update.Has:
		w.WriteHeader(http.StatusOK)

		if cts.Update.Data.ID == "" {
			cts.Update.Data.ID = "foo"
		}
		if cts.Update.Data.ArtifactName() == "" {
			cts.Update.Data.Artifact.ArtifactName = "foo"
		}
		if cts.Update.Data.URI() == "" {
			cts.Update.Data.Artifact.Source.URI = cts.URL + "/download"
		}
		if len(cts.Update.Data.Artifact.CompatibleDevices) == 0 {
			cts.Update.Data.Artifact.CompatibleDevices = []string{"vexpress"}
		}
		var ud struct {
			*datastore.UpdateInfo
		}
		ud.UpdateInfo = &cts.Update.Data
		w.Header().Set("Content-Type", "application/json")
		_ = writeJSON(w, &ud)
	default:
		log.Errorf("Unrecognized update status: %v", cts.Update)
	}
}

func (cts *ClientTestServer) updateReqv2(w http.ResponseWriter, r *http.Request) {

	if !cts.Enterprise {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var current *client.UpdateV2Body
	log.Infof("got update request v2 %v", r)
	cts.Update.Called = true

	if r.Method != "POST" {
		w.WriteHeader(400)
	} else {
		if !cts.verifyAuth(w, r) {
			log.Info("Not authorized")
			return
		}
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(500)
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		err = json.Unmarshal(body, &current)
		if err != nil {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(err.Error()))
			return
		}

		if current.DeviceProvides.Artifact == "" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte("artifact_name missing from payload"))
			return
		}
		if current.DeviceProvides.DeviceType == "" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte("device_type missing from payload"))
			return
		}
		if !reflect.DeepEqual(current.DeviceProvides.Provides, cts.Update.Current.Provides) {
			log.Errorf("incorrect current update info, got %+v, expected %+v",
				current, *cts.Update.Current)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

	}
	switch {
	case cts.Update.Unauthorized:
		w.WriteHeader(http.StatusUnauthorized)
	case !cts.Update.Has:
		w.WriteHeader(http.StatusNoContent)
	case cts.Update.Has:
		w.WriteHeader(http.StatusOK)

		if cts.Update.Data.ID == "" {
			cts.Update.Data.ID = "foo"
		}
		if cts.Update.Data.ArtifactName() == "" {
			cts.Update.Data.Artifact.ArtifactName = "foo"
		}
		if cts.Update.Data.URI() == "" {
			cts.Update.Data.Artifact.Source.URI = cts.URL + "/download"
		}
		if len(cts.Update.Data.Artifact.CompatibleDevices) == 0 {
			cts.Update.Data.Artifact.CompatibleDevices = []string{"vexpress"}
		}
		var ud struct {
			*datastore.UpdateInfo
			ControlMap *updatecontrolmap.UpdateControlMap `json:"update_control_map"`
		}
		ud.UpdateInfo = &cts.Update.Data
		if cts.Update.ControlMap != nil {
			ud.ControlMap = cts.Update.ControlMap
		}
		w.Header().Set("Content-Type", "application/json")
		_ = writeJSON(w, &ud)
	default:
		log.Errorf("Unrecognized update status: %v", cts.Update)
	}
}

func (cts *ClientTestServer) updateDownloadReq(w http.ResponseWriter, r *http.Request) {
	log.Infof("got update download request %v", r)
	cts.UpdateDownload.Called = true

	if !isMethod(http.MethodGet, w, r) {
		return
	}

	// fetch should not carry Authorization header
	hv := r.Header.Get("Authorization")
	if hv != "" {
		w.WriteHeader(http.StatusBadRequest)
	}

	w.Header().Set("Content-Length", strconv.Itoa(cts.UpdateDownload.Data.Len()))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, &cts.UpdateDownload.Data)
}

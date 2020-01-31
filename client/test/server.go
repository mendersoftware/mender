// Copyright 2020 Northern.tech AS
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
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/mendersoftware/log"
	"github.com/mendersoftware/mender/client"
	"github.com/mendersoftware/mender/datastore"
)

type updateType struct {
	Has          bool
	Data         datastore.UpdateInfo
	Unauthorized bool
	Called       bool
	Current      client.CurrentUpdate
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

type ClientTestServer struct {
	*httptest.Server

	Update         updateType
	UpdateDownload updateDownloadType
	Auth           authType
	Status         statusType
	Log            logType
	Inventory      inventoryType
}

func NewClientTestServer() *ClientTestServer {
	cts := &ClientTestServer{}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/devices/v1/authentication/auth_requests", cts.authReq)
	mux.HandleFunc("/api/devices/v1/inventory/device/attributes", cts.inventoryReq)
	mux.HandleFunc("/api/devices/v1/deployments/device/deployments/next", cts.updateReq)
	// mux.HandleFunc("/api/devices/v1/deployments/device/deployments/%s/log", cts.logReq)
	// mux.HandleFunc("/api/devices/v1/deployments/device/deployments/%s/status", cts.statusReq)
	mux.HandleFunc("/api/devices/v1/deployments/device/deployments/", cts.deploymentsReq)
	mux.HandleFunc("/api/devices/v1/download", cts.updateDownloadReq)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Infof("fallback request handler, request %v", r)
		w.WriteHeader(http.StatusBadRequest)
	})

	srv := httptest.NewServer(mux)
	cts.Server = srv

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
			w.Write(cts.Auth.Token)
		}
	} else {
		w.WriteHeader(http.StatusUnauthorized)
	}

}

func (cts *ClientTestServer) inventoryReq(w http.ResponseWriter, r *http.Request) {
	log.Infof("got inventory request %v", r)
	cts.Inventory.Called = true

	if !isMethod(http.MethodPatch, w, r) {
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

func (cts *ClientTestServer) updateReq(w http.ResponseWriter, r *http.Request) {
	log.Infof("got update request %v", r)
	cts.Update.Called = true

	// Enterprise client device provides post is not supported yet
	if r.Method == "POST" {
		w.WriteHeader(404)
		return
	}

	if !isMethod(http.MethodGet, w, r) {
		return
	}

	log.Infof("Valid update request GET: %v", r)

	if !cts.verifyAuth(w, r) {
		return
	}

	log.Infof("parsed URL query: %v", r.URL.Query())

	if current := urlQueryToCurrentUpdate(r.URL.Query()); !reflect.DeepEqual(current, cts.Update.Current) {
		log.Errorf("incorrect current update info, got %+v, expected %+v",
			current, cts.Update.Current)
		w.WriteHeader(http.StatusBadRequest)
		return
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
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, &cts.Update.Data)
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
	io.Copy(w, &cts.UpdateDownload.Data)
}

/*
Copyright © 2024-2026 Ingka Holding B.V. All Rights Reserved.
Licensed under the GPL, Version 3 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

       <https://www.gnu.org/licenses/gpl-3.0.en.html>

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package nutanix

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(isPC bool) *Client {
	return newClient("test-cluster", "https://10.0.0.1:9440", "user", "pass", nil, isPC, true, 10*time.Second)
}

func Test_buildRequest_URL(t *testing.T) {
	tests := []struct {
		name    string
		isPC    bool
		action  string
		wantURL string
	}{
		{
			name:    "PC has no basePath",
			isPC:    true,
			action:  "/api/nutanix/v3/clusters/list",
			wantURL: "https://10.0.0.1:9440/api/nutanix/v3/clusters/list",
		},
		{
			name:    "PE prepends basePath",
			isPC:    false,
			action:  "/v2.0/hosts/",
			wantURL: "https://10.0.0.1:9440/PrismGateway/services/rest/v2.0/hosts",
		},
		{
			name:    "leading and trailing slashes on action are trimmed",
			isPC:    true,
			action:  "///v2.0/vms///",
			wantURL: "https://10.0.0.1:9440/v2.0/vms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(tt.isPC)
			req, err := c.buildRequest(context.Background(), "GET", tt.action, RequestOptions{})
			if err != nil {
				t.Fatalf("buildRequest() unexpected error: %v", err)
			}
			if req.URL.String() != tt.wantURL {
				t.Errorf("URL = %q, want %q", req.URL.String(), tt.wantURL)
			}
		})
	}
}

func Test_buildRequest_QueryParams(t *testing.T) {
	c := newTestClient(true)
	opt := RequestOptions{
		Params: url.Values{
			"$limit": []string{"100"},
			"$page":  []string{"2"},
		},
	}

	req, err := c.buildRequest(context.Background(), "GET", "/api/clusters", opt)
	if err != nil {
		t.Fatalf("buildRequest() unexpected error: %v", err)
	}

	q := req.URL.Query()
	if got := q.Get("$limit"); got != "100" {
		t.Errorf("$limit = %q, want %q", got, "100")
	}
	if got := q.Get("$page"); got != "2" {
		t.Errorf("$page = %q, want %q", got, "2")
	}
}

func Test_buildRequest_Payload(t *testing.T) {
	c := newTestClient(true)

	payload := map[string]any{"kind": "cluster", "length": 100}
	opt := RequestOptions{Payload: payload}

	req, err := c.buildRequest(context.Background(), "POST", "/api/nutanix/v3/clusters/list", opt)
	if err != nil {
		t.Fatalf("buildRequest() unexpected error: %v", err)
	}

	if ct := req.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("failed to read request body: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("request body is not valid JSON: %v", err)
	}
	if got["kind"] != "cluster" {
		t.Errorf("body kind = %v, want %q", got["kind"], "cluster")
	}
}

func Test_buildRequest_NoPayload(t *testing.T) {
	c := newTestClient(true)

	req, err := c.buildRequest(context.Background(), "GET", "/api/clusters", RequestOptions{})
	if err != nil {
		t.Fatalf("buildRequest() unexpected error: %v", err)
	}

	if ct := req.Header.Get("Content-Type"); ct != "" {
		t.Errorf("Content-Type should be empty for no-payload request, got %q", ct)
	}
}

func Test_buildRequest_BasicAuth(t *testing.T) {
	c := newTestClient(true)
	c.username = "myuser"
	c.password = "mypassword"

	req, err := c.buildRequest(context.Background(), "GET", "/api/clusters", RequestOptions{})
	if err != nil {
		t.Fatalf("buildRequest() unexpected error: %v", err)
	}

	u, p, ok := req.BasicAuth()
	if !ok {
		t.Fatal("BasicAuth() returned ok=false, expected basic auth to be set")
	}
	if u != "myuser" {
		t.Errorf("username = %q, want %q", u, "myuser")
	}
	if p != "mypassword" {
		t.Errorf("password = %q, want %q", p, "mypassword")
	}
}

// mockCredentialProvider is a test double for auth.CredentialProvider.
type mockCredentialProvider struct {
	refreshCalled atomic.Int32
	refreshErr    error
	username      string
	password      string
	newUsername   string
	newPassword   string
}

func (m *mockCredentialProvider) Refresh() error {
	m.refreshCalled.Add(1)
	return m.refreshErr
}

func (m *mockCredentialProvider) GetPCCreds(_ string) (string, string, error) {
	if m.refreshCalled.Load() > 0 && m.newUsername != "" {
		return m.newUsername, m.newPassword, nil
	}
	return m.username, m.password, nil
}

func (m *mockCredentialProvider) GetPECreds(_ string) (string, string, error) {
	if m.refreshCalled.Load() > 0 && m.newUsername != "" {
		return m.newUsername, m.newPassword, nil
	}
	return m.username, m.password, nil
}

func newTestClientWithServer(t *testing.T, handler http.HandlerFunc, creds *mockCredentialProvider) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	c := newClient("test-cluster", srv.URL, creds.username, creds.password, creds, true, true, 10*time.Second)
	c.httpClient = srv.Client()
	return c, srv
}

func Test_MakeRequest_Success(t *testing.T) {
	creds := &mockCredentialProvider{username: "user", password: "pass"}
	c, _ := newTestClientWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}, creds)

	resp, err := c.MakeRequest(context.Background(), "GET", "/api/clusters")
	if err != nil {
		t.Fatalf("MakeRequest() unexpected error: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if creds.refreshCalled.Load() != 0 {
		t.Errorf("Refresh() called %d times, want 0", creds.refreshCalled.Load())
	}
}

func Test_MakeRequest_401_RefreshAndRetry(t *testing.T) {
	var requestCount atomic.Int32
	creds := &mockCredentialProvider{username: "newuser", password: "newpass"}

	c, _ := newTestClientWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		if requestCount.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}, creds)

	resp, err := c.MakeRequest(context.Background(), "GET", "/api/clusters")
	if err != nil {
		t.Fatalf("MakeRequest() unexpected error: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if creds.refreshCalled.Load() != 1 {
		t.Errorf("Refresh() called %d times, want 1", creds.refreshCalled.Load())
	}
	if requestCount.Load() != 2 {
		t.Errorf("server received %d requests, want 2", requestCount.Load())
	}
}

func Test_MakeRequest_403_RefreshAndRetry(t *testing.T) {
	var requestCount atomic.Int32
	creds := &mockCredentialProvider{username: "newuser", password: "newpass"}

	c, _ := newTestClientWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		if requestCount.Add(1) == 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}, creds)

	resp, err := c.MakeRequest(context.Background(), "GET", "/api/clusters")
	if err != nil {
		t.Fatalf("MakeRequest() unexpected error: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if creds.refreshCalled.Load() != 1 {
		t.Errorf("Refresh() called %d times, want 1", creds.refreshCalled.Load())
	}
}

func Test_MakeRequest_401_NoRetryOnRefreshError(t *testing.T) {
	creds := &mockCredentialProvider{
		username:   "user",
		password:   "pass",
		refreshErr: http.ErrNoCookie, // any non-nil error
	}

	c, _ := newTestClientWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}, creds)

	resp, err := c.MakeRequest(context.Background(), "GET", "/api/clusters")
	if resp != nil {
		resp.Body.Close() //nolint:errcheck
	}
	if err == nil {
		t.Fatal("MakeRequest() expected error on refresh failure, got nil")
	}
}

func Test_MakeRequest_RefreshUpdatesCredentials(t *testing.T) {
	creds := &mockCredentialProvider{
		username:    "staleuser",
		password:    "stalepass",
		newUsername: "freshuser",
		newPassword: "freshpass",
	}

	var firstRequestAuth, secondRequestAuth string
	var requestCount atomic.Int32

	c, _ := newTestClientWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if count == 1 {
			firstRequestAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		secondRequestAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}, creds)

	resp, err := c.MakeRequest(context.Background(), "GET", "/api/clusters")
	if err != nil {
		t.Fatalf("MakeRequest() unexpected error: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	staleReq := &http.Request{Header: http.Header{}}
	staleReq.SetBasicAuth("staleuser", "stalepass")
	wantStale := staleReq.Header.Get("Authorization")

	freshReq := &http.Request{Header: http.Header{}}
	freshReq.SetBasicAuth("freshuser", "freshpass")
	wantFresh := freshReq.Header.Get("Authorization")

	if firstRequestAuth != wantStale {
		t.Errorf("first request Authorization = %q, want stale credentials %q", firstRequestAuth, wantStale)
	}
	if secondRequestAuth != wantFresh {
		t.Errorf("retry Authorization = %q, want fresh credentials %q", secondRequestAuth, wantFresh)
	}
}

func Test_refreshCreds_ThrottlesConcurrentCallers(t *testing.T) {
	const goroutines = 20

	creds := &mockCredentialProvider{username: "user", password: "pass"}

	// Every request returns 401 so every goroutine hits the refresh path.
	c, _ := newTestClientWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}, creds)

	// Zero out lastRefresh so the first caller actually refreshes.
	c.lastRefresh = time.Time{}

	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			//nolint:errcheck
			c.refreshCreds()
		})
	}
	wg.Wait()

	if n := creds.refreshCalled.Load(); n != 1 {
		t.Errorf("Refresh() called %d times across %d concurrent callers, want 1", n, goroutines)
	}
}

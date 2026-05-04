/*
Copyright © 2024 Ingka Holding B.V. All Rights Reserved.
Licensed under the GPL, Version 2 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

       <https://www.gnu.org/licenses/gpl-2.0.en.html>

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package exporter_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ingka-group/nutanix-exporter/pkg/exporter"
)

// stubCreds satisfies exporter.CredentialProvider without external calls.
type stubCreds struct{}

func (s *stubCreds) GetPCCreds(_ string) (string, string, error) { return "u", "p", nil }
func (s *stubCreds) GetPECreds(_ string) (string, string, error) { return "u", "p", nil }
func (s *stubCreds) Refresh() error                              { return nil }

func newTestExporterService() *exporter.ExporterService {
	cfg := &exporter.Config{
		PrismCentralURL:        "https://pc.example.com",
		PrismCentralName:       "test-pc",
		ClusterRefreshInterval: 0,
		ClusterPrefix:          "prod",
		PCAPIVersion:           "v3",
		ConfigPath:             "/etc/nutanix",
	}
	return exporter.NewExporterService(cfg, &stubCreds{})
}

func Test_NewExporterService_notNil(t *testing.T) {
	es := newTestExporterService()
	if es == nil {
		t.Fatal("NewExporterService() returned nil")
	}
}

func Test_Stop_noServer(t *testing.T) {
	es := newTestExporterService()
	if err := es.Stop(); err != nil {
		t.Errorf("Stop() before Start() returned error: %v", err)
	}
}

func Test_GetHandler_returnsHandler(t *testing.T) {
	es := newTestExporterService()
	h := es.GetHandler()
	if h == nil {
		t.Fatal("GetHandler() returned nil")
	}
}

func Test_GetHandler_respondsOK(t *testing.T) {
	es := newTestExporterService()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	es.GetHandler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GetHandler response = %d, want 200", w.Code)
	}
}

func Test_Config_fields(t *testing.T) {
	cfg := &exporter.Config{
		PrismCentralURL:        "https://pc.example.com",
		PrismCentralName:       "my-pc",
		ClusterRefreshInterval: 5 * time.Minute,
		ClusterPrefix:          "eu-",
		PCAPIVersion:           "v3",
		ConfigPath:             "/config",
	}

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"PrismCentralURL", cfg.PrismCentralURL, "https://pc.example.com"},
		{"PrismCentralName", cfg.PrismCentralName, "my-pc"},
		{"ClusterRefreshInterval", cfg.ClusterRefreshInterval, 5 * time.Minute},
		{"ClusterPrefix", cfg.ClusterPrefix, "eu-"},
		{"PCAPIVersion", cfg.PCAPIVersion, "v3"},
		{"ConfigPath", cfg.ConfigPath, "/config"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("Config.%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

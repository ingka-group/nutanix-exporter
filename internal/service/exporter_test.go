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

package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ingka-group/nutanix-exporter/internal/auth"
	"github.com/ingka-group/nutanix-exporter/internal/config"
	"github.com/ingka-group/nutanix-exporter/internal/nutanix"
	"github.com/prometheus/client_golang/prometheus"
)

// stubCredentialProvider satisfies auth.CredentialProvider without any external calls.
type stubCredentialProvider struct{}

func (s *stubCredentialProvider) GetPCCreds(_ string) (string, string, error) {
	return "user", "pass", nil
}

func (s *stubCredentialProvider) GetPECreds(_ string) (string, string, error) {
	return "user", "pass", nil
}

func (s *stubCredentialProvider) Refresh() error { return nil }

var _ auth.CredentialProvider = (*stubCredentialProvider)(nil)

func newTestService() *ExporterService {
	cfg := &config.Config{
		PrismCentralURL:  "https://pc.example.com",
		PrismCentralName: "test-pc",
	}
	return NewExporterService(cfg, &stubCredentialProvider{})
}

func Test_NewExporterService(t *testing.T) {
	cfg := &config.Config{
		PrismCentralURL:  "https://pc.example.com",
		PrismCentralName: "test-pc",
	}
	cred := &stubCredentialProvider{}

	es := NewExporterService(cfg, cred)

	if es == nil {
		t.Fatal("NewExporterService() returned nil")
	}
	if es.config != cfg {
		t.Error("NewExporterService() config not set")
	}
	if es.credentialProvider != cred {
		t.Error("NewExporterService() credentialProvider not set")
	}
	if es.clustersMap == nil {
		t.Error("NewExporterService() clustersMap is nil")
	}
}

func Test_Stop_nilServer(t *testing.T) {
	es := newTestService()
	if err := es.Stop(); err != nil {
		t.Errorf("Stop() with nil server returned error: %v", err)
	}
}

func Test_Stop_withServer(t *testing.T) {
	es := newTestService()
	es.setupHTTPHandlers()
	// Don't actually listen — just verify Shutdown returns cleanly.
	if err := es.Stop(); err != nil {
		t.Errorf("Stop() with configured server returned error: %v", err)
	}
}

func Test_setupHTTPHandlers(t *testing.T) {
	es := newTestService()
	es.setupHTTPHandlers()

	if es.server == nil {
		t.Fatal("setupHTTPHandlers() left server nil")
	}
	if es.server.Addr != es.config.ListenAddress {
		t.Errorf("server.Addr = %q, want %q", es.server.Addr, es.config.ListenAddress)
	}
	if es.server.ReadTimeout != 10*time.Second {
		t.Errorf("server.ReadTimeout = %v, want 10s", es.server.ReadTimeout)
	}
	if es.server.WriteTimeout != 30*time.Second {
		t.Errorf("server.WriteTimeout = %v, want 30s", es.server.WriteTimeout)
	}
	if es.server.IdleTimeout != 120*time.Second {
		t.Errorf("server.IdleTimeout = %v, want 120s", es.server.IdleTimeout)
	}
}

func Test_indexHandler(t *testing.T) {
	es := newTestService()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	es.indexHandler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("indexHandler status = %d, want 200", resp.StatusCode)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Nutanix Exporter") {
		t.Errorf("indexHandler body missing title, got: %q", body)
	}
	if !strings.Contains(body, "/metrics") {
		t.Errorf("indexHandler body missing metrics link, got: %q", body)
	}
}

func Test_metricsHandler_unknownCluster(t *testing.T) {
	es := newTestService()

	req := httptest.NewRequest(http.MethodGet, "/metrics/nonexistent", nil)
	w := httptest.NewRecorder()
	es.metricsHandler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("metricsHandler unknown cluster = %d, want 404", w.Code)
	}
}

func Test_metricsHandler_knownCluster(t *testing.T) {
	es := newTestService()

	reg := prometheus.NewRegistry()
	es.clustersMap["test-cluster"] = &clusterEntry{
		cluster:  &nutanix.Cluster{Name: "test-cluster"},
		registry: reg,
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics/test-cluster", nil)
	w := httptest.NewRecorder()
	es.metricsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("metricsHandler known cluster = %d, want 200", w.Code)
	}
}

func Test_GetHandler_emptyMap(t *testing.T) {
	es := newTestService()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	es.GetHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GetHandler empty clustersMap = %d, want 200", w.Code)
	}
}

func Test_GetHandler_withClusters(t *testing.T) {
	es := newTestService()

	reg := prometheus.NewRegistry()
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_metric",
		Help: "a test metric",
	})
	gauge.Set(42)
	reg.MustRegister(gauge)

	es.clustersMap["cluster-a"] = &clusterEntry{
		cluster:  &nutanix.Cluster{Name: "cluster-a"},
		registry: reg,
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	es.GetHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GetHandler with cluster = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "test_metric") {
		t.Error("GetHandler response missing registered metric")
	}
}

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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ingka-group/nutanix-exporter/internal/nutanix"
	"github.com/prometheus/client_golang/prometheus"
)

// addCluster registers a cluster with an empty registry so it appears in
// discovery output and is servable by metricsHandler.
func addCluster(es *ExporterService, name string) {
	es.clustersMap[name] = &clusterEntry{
		cluster:  &nutanix.Cluster{Name: name},
		registry: prometheus.NewRegistry(),
	}
}

// requestDiscovery calls the handler and decodes the response.
func requestDiscovery(t *testing.T, es *ExporterService, host string) ([]sdTargetGroup, *httptest.ResponseRecorder) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/sd", nil)
	if host != "" {
		req.Host = host
	}
	w := httptest.NewRecorder()
	es.serviceDiscoveryHandler(w, req)

	var groups []sdTargetGroup
	if err := json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, w.Body.String())
	}
	return groups, w
}

func Test_serviceDiscoveryHandler_ContentType(t *testing.T) {
	es := newTestService()
	addCluster(es, "cluster-a")

	_, w := requestDiscovery(t, es, "exporter:9408")

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	// Prometheus rejects the response unless this is exactly application/json.
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

func Test_serviceDiscoveryHandler_EmptyIsEmptyArray(t *testing.T) {
	es := newTestService()

	req := httptest.NewRequest(http.MethodGet, "/sd", nil)
	w := httptest.NewRecorder()
	es.serviceDiscoveryHandler(w, req)

	// Encoding a nil slice would yield "null", which Prometheus rejects.
	if got := w.Body.String(); got != "[]\n" {
		t.Errorf("body with no clusters = %q, want %q", got, "[]\n")
	}
}

func Test_serviceDiscoveryHandler_TargetsAndLabels(t *testing.T) {
	es := newTestService()
	addCluster(es, "ProdSite1-NXC000")

	groups, _ := requestDiscovery(t, es, "exporter:9408")

	if len(groups) != 1 {
		t.Fatalf("got %d target groups, want 1", len(groups))
	}
	g := groups[0]

	if len(g.Targets) != 1 || g.Targets[0] != "exporter:9408" {
		t.Errorf("targets = %v, want [exporter:9408]", g.Targets)
	}
	if got := g.Labels[metricsPathLabel]; got != "/metrics/ProdSite1-NXC000" {
		t.Errorf("%s = %q, want %q", metricsPathLabel, got, "/metrics/ProdSite1-NXC000")
	}
	if got := g.Labels[clusterMetaLabel]; got != "ProdSite1-NXC000" {
		t.Errorf("%s = %q, want %q", clusterMetaLabel, got, "ProdSite1-NXC000")
	}
	// Without this every cluster would inherit the same instance from __address__.
	if got := g.Labels[instanceLabel]; got != "ProdSite1-NXC000" {
		t.Errorf("%s = %q, want %q", instanceLabel, got, "ProdSite1-NXC000")
	}
}

func Test_serviceDiscoveryHandler_UsesRequestHostByDefault(t *testing.T) {
	es := newTestService()
	addCluster(es, "cluster-a")

	groups, _ := requestDiscovery(t, es, "nutanix-exporter.monitoring.svc:9408")

	if len(groups) != 1 {
		t.Fatalf("got %d target groups, want 1", len(groups))
	}
	if got := groups[0].Targets[0]; got != "nutanix-exporter.monitoring.svc:9408" {
		t.Errorf("target = %q, want the request Host", got)
	}
}

func Test_serviceDiscoveryHandler_SDTargetAddressOverridesHost(t *testing.T) {
	es := newTestService()
	es.config.SDTargetAddress = "public-exporter.example.com:443"
	addCluster(es, "cluster-a")

	groups, _ := requestDiscovery(t, es, "internal-host:9408")

	if got := groups[0].Targets[0]; got != "public-exporter.example.com:443" {
		t.Errorf("target = %q, want the configured override", got)
	}
}

func Test_serviceDiscoveryHandler_SortedByClusterName(t *testing.T) {
	es := newTestService()
	// Inserted out of order; map iteration order is random.
	for _, name := range []string{"cluster-c", "cluster-a", "cluster-b"} {
		addCluster(es, name)
	}

	groups, _ := requestDiscovery(t, es, "exporter:9408")

	if len(groups) != 3 {
		t.Fatalf("got %d target groups, want 3", len(groups))
	}
	want := []string{"cluster-a", "cluster-b", "cluster-c"}
	for i, name := range want {
		if got := groups[i].Labels[clusterMetaLabel]; got != name {
			t.Errorf("group %d cluster = %q, want %q", i, got, name)
		}
	}
}

func Test_metricsPathFor_EscapesUnsafeNames(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "cluster-a", want: "/metrics/cluster-a"},
		{name: "cluster.name", want: "/metrics/cluster.name"},
		{name: "cluster name", want: "/metrics/cluster%20name"},
		{name: "cluster?x=1", want: "/metrics/cluster%3Fx=1"},
		{name: "cluster#frag", want: "/metrics/cluster%23frag"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := metricsPathFor(tt.name); got != tt.want {
				t.Errorf("metricsPathFor(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// The advertised path is only useful if it actually resolves back to the right
// cluster, so drive the discovered path through the real mux.
func Test_DiscoveredPathsResolveToTheirCluster(t *testing.T) {
	names := []string{
		"cluster-a",
		"cluster.with.dots",
		"cluster name with spaces",
		"cluster?x=1",
	}

	es := newTestService()
	for _, name := range names {
		addCluster(es, name)
	}
	es.setupHTTPHandlers()

	groups, _ := requestDiscovery(t, es, "exporter:9408")
	if len(groups) != len(names) {
		t.Fatalf("got %d target groups, want %d", len(groups), len(names))
	}

	for _, g := range groups {
		path := g.Labels[metricsPathLabel]
		cluster := g.Labels[clusterMetaLabel]

		t.Run(cluster, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			es.server.Handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("GET %q for cluster %q = %d, want 200", path, cluster, w.Code)
			}
		})
	}
}

func Test_DiscoveredPathForUnknownClusterIs404(t *testing.T) {
	es := newTestService()
	addCluster(es, "cluster-a")
	es.setupHTTPHandlers()

	req := httptest.NewRequest(http.MethodGet, metricsPathFor("cluster-b"), nil)
	w := httptest.NewRecorder()
	es.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("unknown cluster path = %d, want 404", w.Code)
	}
}

func Test_setupHTTPHandlers_ServesDiscoveryRoute(t *testing.T) {
	es := newTestService()
	addCluster(es, "cluster-a")
	es.setupHTTPHandlers()

	req := httptest.NewRequest(http.MethodGet, "/sd", nil)
	w := httptest.NewRecorder()
	es.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /sd = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func Test_GetServiceDiscoveryHandler(t *testing.T) {
	es := newTestService()
	addCluster(es, "cluster-a")

	req := httptest.NewRequest(http.MethodGet, "/sd", nil)
	w := httptest.NewRecorder()
	es.GetServiceDiscoveryHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GetServiceDiscoveryHandler = %d, want 200", w.Code)
	}

	var groups []sdTargetGroup
	if err := json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if len(groups) != 1 {
		t.Errorf("got %d target groups, want 1", len(groups))
	}
}

func Test_GetMetricsHandler(t *testing.T) {
	es := newTestService()
	addCluster(es, "cluster-a")

	req := httptest.NewRequest(http.MethodGet, "/metrics/cluster-a", nil)
	w := httptest.NewRecorder()
	es.GetMetricsHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GetMetricsHandler = %d, want 200", w.Code)
	}
}

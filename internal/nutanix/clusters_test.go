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
	"strings"
	"testing"
)

// mockClient serves a fixed JSON body for every request, which is enough for
// FetchClusters: a page holding fewer than 100 clusters ends pagination.
type mockClient struct {
	body string
}

func (m *mockClient) MakeRequest(_ context.Context, _, _ string, _ ...RequestOptions) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(m.body)),
	}, nil
}

const (
	pcApplianceName = "ProdCentral-NXP000"
	peClusterName   = "ProdSite1-NXC000"
)

// The PC appliance and one PE cluster, shaped as Prism Central reports them.
const (
	clustersV4Body = `{
		"metadata": {"totalAvailableResults": 2},
		"data": [
			{
				"name": "ProdCentral-NXP000",
				"network": {"externalAddress": {"ipv4": {"value": "10.0.0.1"}}},
				"config": {"clusterFunction": ["PRISM_CENTRAL"]}
			},
			{
				"name": "ProdSite1-NXC000",
				"network": {"externalAddress": {"ipv4": {"value": "10.0.0.2"}}},
				"config": {"clusterFunction": ["AOS"]}
			}
		]
	}`

	clustersV3Body = `{
		"metadata": {"total_matches": 2},
		"entities": [
			{
				"spec": {"name": "ProdCentral-NXP000"},
				"status": {"resources": {
					"network": {"external_ip": "10.0.0.1"},
					"config": {"service_list": ["PRISM_CENTRAL"]}
				}}
			},
			{
				"spec": {"name": "ProdSite1-NXC000"},
				"status": {"resources": {
					"network": {"external_ip": "10.0.0.2"},
					"config": {"service_list": ["AOS"]}
				}}
			}
		]
	}`
)

func decodeBody(t *testing.T, body string) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("test fixture is not valid JSON: %v", err)
	}
	return result
}

func Test_hasPrismCentralFunction(t *testing.T) {
	tests := []struct {
		name      string
		functions any
		want      bool
	}{
		{name: "PRISM_CENTRAL present", functions: []any{"PRISM_CENTRAL"}, want: true},
		{name: "PRISM_CENTRAL alongside others", functions: []any{"AOS", "PRISM_CENTRAL"}, want: true},
		{name: "case insensitive", functions: []any{"prism_central"}, want: true},
		{name: "AOS only", functions: []any{"AOS"}, want: false},
		{name: "empty list", functions: []any{}, want: false},
		{name: "missing field", functions: nil, want: false},
		{name: "not a list", functions: "PRISM_CENTRAL", want: false},
		{name: "non-string entries ignored", functions: []any{42, true}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasPrismCentralFunction(tt.functions); got != tt.want {
				t.Errorf("hasPrismCentralFunction(%v) = %v, want %v", tt.functions, got, tt.want)
			}
		})
	}
}

func Test_parseClustersV4_DetectsPrismCentral(t *testing.T) {
	clusters, total, err := parseClustersV4(decodeBody(t, clustersV4Body))
	if err != nil {
		t.Fatalf("parseClustersV4() unexpected error: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(clusters) != 2 {
		t.Fatalf("parsed %d clusters, want 2", len(clusters))
	}
	if !clusters[0].isPC {
		t.Errorf("%q isPC = false, want true", clusters[0].name)
	}
	if clusters[1].isPC {
		t.Errorf("%q isPC = true, want false", clusters[1].name)
	}
}

func Test_parseClustersV4_MissingConfigIsNotPC(t *testing.T) {
	body := `{
		"metadata": {"totalAvailableResults": 1},
		"data": [{
			"name": "no-config",
			"network": {"externalAddress": {"ipv4": {"value": "10.0.0.1"}}}
		}]
	}`

	clusters, _, err := parseClustersV4(decodeBody(t, body))
	if err != nil {
		t.Fatalf("parseClustersV4() unexpected error: %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("parsed %d clusters, want 1", len(clusters))
	}
	if clusters[0].isPC {
		t.Error("cluster with no config field marked as PC, want it kept as PE")
	}
}

func Test_parseClustersV3_DetectsPrismCentral(t *testing.T) {
	clusters, total, err := parseClustersV3(decodeBody(t, clustersV3Body))
	if err != nil {
		t.Fatalf("parseClustersV3() unexpected error: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(clusters) != 2 {
		t.Fatalf("parsed %d clusters, want 2", len(clusters))
	}
	if !clusters[0].isPC {
		t.Errorf("%q isPC = false, want true", clusters[0].name)
	}
	if clusters[1].isPC {
		t.Errorf("%q isPC = true, want false", clusters[1].name)
	}
}

func Test_parseClustersV3_MissingConfigIsNotPC(t *testing.T) {
	body := `{
		"metadata": {"total_matches": 1},
		"entities": [{
			"spec": {"name": "no-config"},
			"status": {"resources": {"network": {"external_ip": "10.0.0.1"}}}
		}]
	}`

	clusters, _, err := parseClustersV3(decodeBody(t, body))
	if err != nil {
		t.Fatalf("parseClustersV3() unexpected error: %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("parsed %d clusters, want 1", len(clusters))
	}
	if clusters[0].isPC {
		t.Error("cluster with no config field marked as PC, want it kept as PE")
	}
}

func Test_FetchClusters_SkipPCAppliance(t *testing.T) {
	tests := []struct {
		name       string
		apiVersion string
		body       string
	}{
		{name: "v4", apiVersion: "v4", body: clustersV4Body},
		{name: "v4b1", apiVersion: "v4b1", body: clustersV4Body},
		{name: "v3", apiVersion: "v3", body: clustersV3Body},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/included by default", func(t *testing.T) {
			client := &mockClient{body: tt.body}
			got, err := FetchClusters(context.Background(), client, FetchOptions{APIVersion: tt.apiVersion})
			if err != nil {
				t.Fatalf("FetchClusters() unexpected error: %v", err)
			}

			want := map[string]string{
				pcApplianceName: "https://10.0.0.1:9440",
				peClusterName:   "https://10.0.0.2:9440",
			}
			if len(got) != len(want) {
				t.Fatalf("got %d clusters, want %d: %v", len(got), len(want), got)
			}
			for name, url := range want {
				if got[name] != url {
					t.Errorf("cluster %q = %q, want %q", name, got[name], url)
				}
			}
		})

		t.Run(tt.name+"/excluded when skipping", func(t *testing.T) {
			client := &mockClient{body: tt.body}
			got, err := FetchClusters(context.Background(), client, FetchOptions{
				APIVersion:      tt.apiVersion,
				SkipPCAppliance: true,
			})
			if err != nil {
				t.Fatalf("FetchClusters() unexpected error: %v", err)
			}

			if _, exists := got[pcApplianceName]; exists {
				t.Errorf("PC appliance %q present, want it skipped", pcApplianceName)
			}
			if got[peClusterName] != "https://10.0.0.2:9440" {
				t.Errorf("PE cluster %q = %q, want it retained", peClusterName, got[peClusterName])
			}
			if len(got) != 1 {
				t.Errorf("got %d clusters, want 1: %v", len(got), got)
			}
		})
	}
}

func Test_FetchClusters_SkipPCApplianceWithPrefix(t *testing.T) {
	client := &mockClient{body: clustersV4Body}
	got, err := FetchClusters(context.Background(), client, FetchOptions{
		APIVersion:      "v4",
		Prefix:          "Prod",
		SkipPCAppliance: true,
	})
	if err != nil {
		t.Fatalf("FetchClusters() unexpected error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d clusters, want 1: %v", len(got), got)
	}
	if _, exists := got[peClusterName]; !exists {
		t.Errorf("expected %q to survive both filters, got %v", peClusterName, got)
	}
}

func Test_FetchClusters_PrefixExcludesEverything(t *testing.T) {
	client := &mockClient{body: clustersV4Body}
	got, err := FetchClusters(context.Background(), client, FetchOptions{
		APIVersion: "v4",
		Prefix:     "OTHER",
	})
	if err != nil {
		t.Fatalf("FetchClusters() unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d clusters, want 0: %v", len(got), got)
	}
}

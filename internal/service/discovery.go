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
	"log/slog"
	"net/http"
	"net/url"
	"sort"
)

const (
	// metricsPathLabel tells Prometheus which path to scrape a target on. Every
	// cluster shares one exporter address and is distinguished only by this path.
	metricsPathLabel = "__metrics_path__"

	// clusterMetaLabel exposes the cluster name for use in relabel_configs. The
	// "__meta_" prefix follows the convention for service discovery metadata:
	// Prometheus drops it after relabeling unless it is explicitly kept.
	clusterMetaLabel = "__meta_nutanix_cluster"

	// instanceLabel is set explicitly because every target here resolves to the
	// same address. Left to default, Prometheus would derive instance from
	// __address__ and every cluster would collide on one instance value.
	instanceLabel = "instance"
)

// sdTargetGroup is a single entry in a Prometheus http_sd_config response.
// See https://prometheus.io/docs/prometheus/latest/configuration/configuration/#http_sd_config
type sdTargetGroup struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

// serviceDiscoveryHandler serves the discovered clusters as Prometheus http_sd
// target groups, replacing a hand-maintained list of per-cluster scrape jobs.
//
// The list is only as fresh as the last cluster refresh, so setting a Prometheus
// refresh_interval shorter than CLUSTER_REFRESH_INTERVAL re-reads the same data.
func (es *ExporterService) serviceDiscoveryHandler(w http.ResponseWriter, r *http.Request) {
	groups := es.discoveryTargets(es.sdTargetAddress(r))

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(groups); err != nil {
		// The status line is already sent, so this can only be logged.
		slog.Error("Failed to encode service discovery response", "error", err)
	}
}

// sdTargetAddress returns the address Prometheus should scrape. It defaults to
// the Host of the discovery request itself. EXPORTER_SD_TARGET overrides it for deployments
// where the scrape address differs from the discovery address.
func (es *ExporterService) sdTargetAddress(r *http.Request) string {
	if es.config.SDTargetAddress != "" {
		return es.config.SDTargetAddress
	}
	return r.Host
}

// discoveryTargets builds one target group per known cluster, sorted by cluster
// name so the response is stable between requests.
func (es *ExporterService) discoveryTargets(address string) []sdTargetGroup {
	es.clustersMu.RLock()
	names := make([]string, 0, len(es.clustersMap))
	for name := range es.clustersMap {
		names = append(names, name)
	}
	es.clustersMu.RUnlock()

	sort.Strings(names)

	// Built empty rather than nil so an exporter with no clusters encodes as []
	// instead of null, which Prometheus rejects.
	groups := make([]sdTargetGroup, 0, len(names))
	for _, name := range names {
		groups = append(groups, sdTargetGroup{
			Targets: []string{address},
			Labels: map[string]string{
				metricsPathLabel: metricsPathFor(name),
				clusterMetaLabel: name,
				instanceLabel:    name,
			},
		})
	}

	return groups
}

// metricsPathFor returns the scrape path for a cluster. The name is escaped so
// that characters which are not URL-safe survive the round trip back to
// metricsHandler, which looks the cluster up by the decoded path.
func metricsPathFor(name string) string {
	return "/metrics/" + url.PathEscape(name)
}

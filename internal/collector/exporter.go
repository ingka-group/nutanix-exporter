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

package collector

import (
	"context"
	"encoding/json"
	"log/slog"

	"os"
	"time"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ingka-group/nutanix-exporter/internal/nutanix"

	"github.com/prometheus/client_golang/prometheus"
	"gopkg.in/yaml.v3"
)

// MetricConfig represents one metric in the config file
type MetricConfig struct {
	Name string `yaml:"name"`
	Help string `yaml:"help"`
}

// Exporter is the struct that gets extended by all other exporters
type Exporter struct {
	clusterName string
	api         nutanix.NutanixClient
	apiPath     string
	metrics     map[string]*prometheus.GaugeVec
	labels      []string
}

// NewExporter is the constructor for Exporter
func NewExporter(clusterName string, api nutanix.NutanixClient, apiPath string, labels []string) *Exporter {
	return &Exporter{
		clusterName: clusterName,
		api:         api,
		apiPath:     apiPath,
		metrics:     make(map[string]*prometheus.GaugeVec),
		labels:      labels,
	}
}

// valueToFloat64 converts a value to float64. Strings "on"/"off" (case-insensitive)
// map to 1/0; other strings are parsed as floats.
func valueToFloat64(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case bool:
		if v {
			return 1.0
		}
		return 0.0
	case string:
		if strings.EqualFold(v, "on") {
			return 1.0
		}
		if strings.EqualFold(v, "off") {
			return 0.0
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return 0
}

// normalizeKey normalizes given key to lowercase and replaces . and - with _
func normalizeKey(key string) string {
	return strings.ToLower(strings.NewReplacer(".", "_", "-", "_", ":", "_").Replace(key))
}

// flattenMap flattens a nested map into a flat map with keys separated by underscores
func (e *Exporter) flattenMap(prefix string, nestedMap map[string]any) map[string]any {

	flatMap := make(map[string]any)
	for key, value := range nestedMap {
		flatKey := key
		if prefix != "" {
			flatKey = prefix + "_" + key
		}
		if nested, ok := value.(map[string]any); ok {
			for k, v := range e.flattenMap(flatKey, nested) {
				flatMap[k] = v
			}
		} else {
			flatMap[flatKey] = value
		}
	}
	return flatMap
}

// Describe implements prometheus.Collector.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, gaugeVec := range e.metrics {
		gaugeVec.Describe(ch)
	}
}

// Collect implements prometheus.Collector.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := e.api.MakeRequest(ctx, "GET", e.apiPath)
	if err != nil {
		slog.Error("Error fetching data", "path", e.apiPath, "cluster", e.clusterName, "error", err)
		return
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Error("Error closing response body", "path", e.apiPath, "cluster", e.clusterName, "error", cerr)
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Error("Error fetching data", "path", e.apiPath, "cluster", e.clusterName, "status", resp.Status)
		return
	}

	var result map[string]any
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Error("Error decoding response", "path", e.apiPath, "cluster", e.clusterName, "error", err)
		return
	}

	e.updateMetrics(result)

	for _, gaugeVec := range e.metrics {
		gaugeVec.Collect(ch)
	}
}

// initMetrics populates e.metrics from parsed YAML bytes and the subsystem name.
func (e *Exporter) initMetrics(subsystem string, data []byte, labelNames []string) error {
	var metrics []MetricConfig
	if err := yaml.Unmarshal(data, &metrics); err != nil {
		return err
	}

	for _, m := range metrics {
		e.metrics[m.Name] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "nutanix",
				Subsystem: subsystem,
				Name:      m.Name,
				Help:      m.Help,
			},
			labelNames,
		)
	}

	return nil
}

// initMetricsFromFile reads configPath and delegates to initMetrics.
func (e *Exporter) initMetricsFromFile(configPath string, labelNames []string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	subsystem := strings.TrimSuffix(filepath.Base(configPath), filepath.Ext(configPath))
	return e.initMetrics(subsystem, data, labelNames)
}

// updateMetrics processes the JSON structure for hosts and updates the metrics.
func (e *Exporter) updateMetrics(data map[string]any) {
	// Check if metadata exists and process it
	if metadata, ok := data["metadata"].(map[string]any); ok {
		e.processMetadata(metadata)
	}

	// Check if the "entities" key is present and is a list
	if entities, ok := data["entities"].([]any); ok {
		// Iterate over the list of entities and process each one
		for _, entity := range entities {
			if ent, ok := entity.(map[string]any); ok {
				e.processEntity(ent, false)
			}
		}
	} else {
		// Cluster API is currently the only API that does not return a list of entities
		// Subsequently, we process the entire cluster as a single entity
		// isCluster flag removes the entity name from the labels
		e.processEntity(data, true)
	}
}

// processEntity handles the processing of a single entity (either a regular entity or the entire cluster)
func (e *Exporter) processEntity(ent map[string]any, isCluster bool) {
	// Flatten the map (recursively) to get a flat map with nested keys separated by underscores
	flatEntity := e.flattenMap("", ent)

	// Iterate over the flattened map and update the metrics
	for key, value := range flatEntity {
		// Normalize the key and check if we're collecting this metric
		normKey := normalizeKey(key)
		if g, exists := e.metrics[normKey]; exists {
			// Set label values and update the metric
			var labelValues []string

			if isCluster {
				// clustername is the only label for cluster-level metrics
				labelValues = []string{e.clusterName}
			} else {
				// For entity-level metrics, use both cluster name and entity name as labels
				if name, ok := ent["name"].(string); ok {
					labelValues = []string{e.clusterName, name}
					// Check for vmname key if name key is not present (used in VMv1 API)
				} else if name, ok := ent["vmName"].(string); ok {
					labelValues = []string{e.clusterName, name}
				} else {
					// Handle case where "name" is missing or not a string
					labelValues = []string{e.clusterName, "unknown"}
				}
			}
			g.WithLabelValues(labelValues...).Set(valueToFloat64(value))
		}
	}
}

// processMetadata handles the processing of metadata for responses that contain an entity list
func (e *Exporter) processMetadata(metadata map[string]any) {
	// Flatten the map (recursively) to get a flat map with nested keys separated by underscores
	flatMetadata := e.flattenMap("", metadata)
	for key, value := range flatMetadata {
		// Normalize the key and check if we're collecting this metric
		normKey := normalizeKey(key)
		if g, exists := e.metrics[normKey]; exists {
			// Set label values and update the metric
			g.WithLabelValues(e.clusterName, "N/A").Set(valueToFloat64(value))
		}
	}
}

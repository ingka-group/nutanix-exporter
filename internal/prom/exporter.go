/*
Copyright © 2024 Ingka Holding B.V. All Rights Reserved.
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

package prom

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"os"
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
	Cluster *nutanix.Cluster                // Reference to the parent Cluster struct
	Metrics map[string]*prometheus.GaugeVec // Holds the metrics defined by the exporter
	Labels  []string                        // Common labels for the metrics
}

// NewExporter is the constructor for Exporter
func NewExporter(cluster *nutanix.Cluster, labels []string) *Exporter {
	return &Exporter{
		Cluster: cluster,
		Metrics: make(map[string]*prometheus.GaugeVec),
		Labels:  labels,
	}
}

// valueToFloat64 converts given value to Float64
// If the value is a string, it will be checked for "on" and "off" and converted to 1 and 0 respectively
// Otherwise it will be parsed as a float64
func (e *Exporter) valueToFloat64(value interface{}) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case string:
		if v == "on" {
			return 1.0
		} else if v == "off" || v == "OFF" {
			return 0.0
		} else if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return 0
}

// normalizeKey normalizes given key to lowercase and replaces . and - with _
func (e *Exporter) normalizeKey(key string) string {
	return strings.ToLower(strings.NewReplacer(".", "_", "-", "_", ":", "_").Replace(key))
}

// flattenMap flattens a nested map into a flat map with keys separated by underscores
func (e *Exporter) flattenMap(prefix string, nestedMap map[string]interface{}) map[string]interface{} {

	flatMap := make(map[string]interface{})
	for key, value := range nestedMap {
		flatKey := key
		if prefix != "" {
			flatKey = prefix + "_" + key
		}
		if nested, ok := value.(map[string]interface{}); ok {
			for k, v := range e.flattenMap(flatKey, nested) {
				flatMap[k] = v
			}
		} else {
			flatMap[flatKey] = value
		}
	}
	return flatMap
}

// Describe method required by prometheus.Collector interface
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, gaugeVec := range e.Metrics {
		gaugeVec.Describe(ch)
	}
}

// fetchData makes a GET request to the given path and returns the response body as a map
func (e *Exporter) fetchData(ctx context.Context, path string) (map[string]interface{}, error) {

	if e.Cluster.RefreshNeeded {
		return nil, fmt.Errorf("skipping %s due to known stale creds", e.Cluster.Name)
	}

	resp, err := e.Cluster.API.MakeRequest(ctx, "GET", path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 || resp.StatusCode == 401 {
		e.Cluster.Mutex.Lock()
		if !e.Cluster.RefreshNeeded {
			log.Printf("Marking stale credentials for refresh for cluster %s", e.Cluster.Name)
			e.Cluster.RefreshNeeded = true
		}
		e.Cluster.Mutex.Unlock()
		return nil, fmt.Errorf("authentication failed for cluster %s", e.Cluster.Name)
	} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("request failed: %s", resp.Status)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("Error decoding response body: %v\n", err)
		return nil, err
	}

	return result, nil
}

// initMetrics initializes metrics based on the provided config file and labels.
func (e *Exporter) initMetrics(configPath string, labelNames []string) error {
	yamlFile, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	var metrics []MetricConfig
	err = yaml.Unmarshal(yamlFile, &metrics)
	if err != nil {
		return err
	}

	// Use the filename without extension as the subsystem
	subsystem := strings.TrimSuffix(filepath.Base(configPath), filepath.Ext(configPath))

	for _, m := range metrics {
		e.Metrics[m.Name] = prometheus.NewGaugeVec(
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

// updateMetrics processes the JSON structure for hosts and updates the metrics.
func (e *Exporter) updateMetrics(data map[string]interface{}) {
	// Check if metadata exists and process it
	if metadata, ok := data["metadata"].(map[string]interface{}); ok {
		e.processMetadata(metadata)
	}

	// Check if the "entities" key is present and is a list
	if entities, ok := data["entities"].([]interface{}); ok {
		// Iterate over the list of entities and process each one
		for _, entity := range entities {
			if ent, ok := entity.(map[string]interface{}); ok {
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
func (e *Exporter) processEntity(ent map[string]interface{}, isCluster bool) {
	// Flatten the map (recursively) to get a flat map with nested keys separated by underscores
	flatEntity := e.flattenMap("", ent)

	// Iterate over the flattened map and update the metrics
	for key, value := range flatEntity {
		// Normalize the key and check if we're collecting this metric
		normKey := e.normalizeKey(key)
		if g, exists := e.Metrics[normKey]; exists {
			// Set label values and update the metric
			var labelValues []string

			if isCluster {
				// clustername is the only label for cluster-level metrics
				labelValues = []string{e.Cluster.Name}
			} else {
				// For entity-level metrics, use both cluster name and entity name as labels
				if name, ok := ent["name"].(string); ok {
					labelValues = []string{e.Cluster.Name, name}
				} else {
					// Handle case where "name" is missing or not a string
					labelValues = []string{e.Cluster.Name, "unknown"}
				}
			}
			g.WithLabelValues(labelValues...).Set(e.valueToFloat64(value))
		}
	}
}

// processMetadata handles the processing of metadata for responses that contain an entity list
func (e *Exporter) processMetadata(metadata map[string]interface{}) {
	// Flatten the map (recursively) to get a flat map with nested keys separated by underscores
	flatMetadata := e.flattenMap("", metadata)
	for key, value := range flatMetadata {
		// Normalize the key and check if we're collecting this metric
		normKey := e.normalizeKey(key)
		if g, exists := e.Metrics[normKey]; exists {
			// Set label values and update the metric
			g.WithLabelValues(e.Cluster.Name, "N/A").Set(e.valueToFloat64(value))
		}
	}
}

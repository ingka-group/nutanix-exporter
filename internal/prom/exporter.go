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

	"nutanix-exporter/internal/nutanix"

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
		} else if v == "off" {
			return 0.0
		} else if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return 0
}

// normalizeKey normalizes given key to lowercase and replaces . and - with _
func (e *Exporter) normalizeKey(key string) string {
	return strings.ToLower(strings.NewReplacer(".", "_", "-", "_").Replace(key))
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

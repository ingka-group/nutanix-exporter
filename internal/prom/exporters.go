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
	"log"
	"time"

	"github.com/ingka-group/nutanix-exporter/internal/nutanix"

	"github.com/prometheus/client_golang/prometheus"
)

// ----- Type Definitions ----- //

type ClusterExporter struct {
	*Exporter
}

type HostsExporter struct {
	*Exporter
}

type VmExporter struct {
	*Exporter
}

type StorageContainerExporter struct {
	*Exporter
}

// ----- Constructors ----- //

func NewClusterCollector(cluster *nutanix.Cluster, configPath string) *ClusterExporter {
	labels := []string{"cluster_name"}
	exporter := &ClusterExporter{
		Exporter: NewExporter(cluster, labels),
	}
	exporter.initMetrics(configPath, labels)
	return exporter
}

func NewHostCollector(cluster *nutanix.Cluster, configPath string) *HostsExporter {
	labels := []string{"cluster_name", "host_name"}
	exporter := &HostsExporter{
		Exporter: NewExporter(cluster, labels),
	}
	exporter.initMetrics(configPath, labels)
	return exporter
}

func NewVMCollector(cluster *nutanix.Cluster, configPath string) *VmExporter {
	labels := []string{"cluster_name", "vm_name"}
	exporter := &VmExporter{
		Exporter: NewExporter(cluster, labels),
	}
	exporter.initMetrics(configPath, labels)
	return exporter
}

func NewStorageContainerCollector(cluster *nutanix.Cluster, configPath string) *StorageContainerExporter {
	labels := []string{"cluster_name", "container_name"}
	exporter := &StorageContainerExporter{
		Exporter: NewExporter(cluster, labels),
	}
	exporter.initMetrics(configPath, labels)
	return exporter
}

// ----- Collect Methods ----- //

// Collect
func (e *StorageContainerExporter) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := e.fetchData(ctx, "/v2.0/storage_containers/")
	if err != nil {
		log.Printf("Error fetching storage container data: %v", err)
		return
	}

	e.updateMetrics(result)

	for _, gaugeVec := range e.Metrics {
		gaugeVec.Collect(ch)
	}
}

// Collect
func (e *ClusterExporter) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := e.fetchData(ctx, "/v2.0/cluster/")
	if err != nil {
		log.Printf("Error fetching cluster data: %v", err)
		return
	}

	e.updateMetrics(result)

	for _, gaugeVec := range e.Metrics {
		gaugeVec.Collect(ch)
	}
}

// Collect
func (e *HostsExporter) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := e.fetchData(ctx, "/v2.0/hosts/")
	if err != nil {
		log.Printf("Error fetching host data: %v", err)
		return
	}

	e.updateMetrics(result)

	for _, gaugeVec := range e.Metrics {
		gaugeVec.Collect(ch)
	}
}

// Collect
func (e *VmExporter) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := e.fetchData(ctx, "/v2.0/vms/")
	if err != nil {
		log.Printf("Error fetching VM data: %v", err)
		return
	}

	e.updateMetrics(result)

	for _, gaugeVec := range e.Metrics {
		gaugeVec.Collect(ch)
	}
}

// ----- Update Methods ----- //

// updateMetrics processes the JSON structure for storage containers and updates the metrics.
func (e *StorageContainerExporter) updateMetrics(data map[string]interface{}) {
	log.Printf("Updating storage container metrics")
	// data has 2 keys: metadata and entities
	// entities is a list of maps with keys: "compression_enabled", "usage_stats", etc. No nested keys
	if entities, ok := data["entities"].([]interface{}); ok {
		for _, entity := range entities {
			if ent, ok := entity.(map[string]interface{}); ok {
				containerName, _ := ent["name"].(string)

				if usageStats, ok := ent["usage_stats"].(map[string]interface{}); ok {
					for key, value := range usageStats {
						normKey := e.normalizeKey(key)
						if g, exists := e.Metrics[normKey]; exists {
							labelValues := []string{e.Cluster.Name, containerName}
							g.WithLabelValues(labelValues...).Set(e.valueToFloat64(value))
						}
					}
				}
			}
		}
	}
}

// updateMetrics searches the JSON structure specific to clusters for defined metrics and updates them
func (e *ClusterExporter) updateMetrics(data map[string]interface{}) {
	log.Printf("Updating cluster metrics")
	// data has many keys
	// some values we care about are at top level, the rest are nested in stats and usage_stats
	for key, value := range data {
		normKey := e.normalizeKey(key)
		if g, exists := e.Metrics[normKey]; exists {
			labelValues := []string{e.Cluster.Name}
			g.WithLabelValues(labelValues...).Set(e.valueToFloat64(value))
		}

		// Check inside nested keys
		if stats, ok := value.(map[string]interface{}); ok {
			for key, value := range stats {
				normKey := e.normalizeKey(key)
				if g, exists := e.Metrics[normKey]; exists {
					labelValues := []string{e.Cluster.Name}
					g.WithLabelValues(labelValues...).Set(e.valueToFloat64(value))
				}
			}
		}
	}
}

// updateMetrics searches the JSON structure specific to hosts for defined metrics and updates them
func (e *HostsExporter) updateMetrics(data map[string]interface{}) {
	log.Printf("Updating host metrics")
	// data has 2 keys: metadata and entities
	// entities is a list of maps for unique hosts with keys like num_cpu_cores + nested keys like stats and usage_stats
	if entities, ok := data["entities"].([]interface{}); ok {
		for _, entity := range entities {
			if ent, ok := entity.(map[string]interface{}); ok {
				hostName, _ := ent["name"].(string)
				// Iterate over all keys in the entity
				for key, value := range ent {
					normKey := e.normalizeKey(key)
					if g, exists := e.Metrics[normKey]; exists {
						labelValues := []string{e.Cluster.Name, hostName}
						g.WithLabelValues(labelValues...).Set(e.valueToFloat64(value))
					}

					// Check inside nested keys
					if stats, ok := value.(map[string]interface{}); ok {
						for key, value := range stats {
							normKey := e.normalizeKey(key)
							if g, exists := e.Metrics[normKey]; exists {
								labelValues := []string{e.Cluster.Name, hostName}
								g.WithLabelValues(labelValues...).Set(e.valueToFloat64(value))
							}
						}
					}
				}
			}
		}
	}
}

// updateMetrics searches the JSON structure specific to VMs for defined metrics and updates them
func (e *VmExporter) updateMetrics(data map[string]interface{}) {
	log.Printf("Updating VM metrics")
	// data has 2 keys: metadata and entities
	// entities is a list of maps for unique VMs, no important nested keys
	if entities, ok := data["entities"].([]interface{}); ok {
		for _, entity := range entities {
			if ent, ok := entity.(map[string]interface{}); ok {
				vmName, _ := ent["name"].(string)
				// Iterate over all keys in the entity
				for key, value := range ent {
					normKey := e.normalizeKey(key)
					if g, exists := e.Metrics[normKey]; exists {
						labelValues := []string{e.Cluster.Name, vmName}
						g.WithLabelValues(labelValues...).Set(e.valueToFloat64(value))
					}
				}
			}
		}
	}
}

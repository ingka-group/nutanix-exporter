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

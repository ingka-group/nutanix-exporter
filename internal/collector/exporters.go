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
	"github.com/ingka-group/nutanix-exporter/internal/nutanix"
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

type Vmv1Exporter struct {
	*Exporter
}

type StorageContainerExporter struct {
	*Exporter
}

// ----- Constructors ----- //

func NewClusterCollector(clusterName string, api nutanix.NutanixClient, configPath string) (*ClusterExporter, error) {
	labels := []string{"cluster_name"}
	exporter := &ClusterExporter{
		Exporter: NewExporter(clusterName, api, "/v2.0/cluster/", labels),
	}
	if err := exporter.initMetrics(configPath, labels); err != nil {
		return nil, err
	}
	return exporter, nil
}

func NewHostCollector(clusterName string, api nutanix.NutanixClient, configPath string) (*HostsExporter, error) {
	labels := []string{"cluster_name", "host_name"}
	exporter := &HostsExporter{
		Exporter: NewExporter(clusterName, api, "/v2.0/hosts/", labels),
	}
	if err := exporter.initMetrics(configPath, labels); err != nil {
		return nil, err
	}
	return exporter, nil
}

func NewVMCollector(clusterName string, api nutanix.NutanixClient, configPath string) (*VmExporter, error) {
	labels := []string{"cluster_name", "vm_name"}
	exporter := &VmExporter{
		Exporter: NewExporter(clusterName, api, "/v2.0/vms/", labels),
	}
	if err := exporter.initMetrics(configPath, labels); err != nil {
		return nil, err
	}
	return exporter, nil
}

func NewVMv1Collector(clusterName string, api nutanix.NutanixClient, configPath string) (*Vmv1Exporter, error) {
	labels := []string{"cluster_name", "vm_name"}
	exporter := &Vmv1Exporter{
		Exporter: NewExporter(clusterName, api, "/v1/vms/", labels),
	}
	if err := exporter.initMetrics(configPath, labels); err != nil {
		return nil, err
	}
	return exporter, nil
}

func NewStorageContainerCollector(clusterName string, api nutanix.NutanixClient, configPath string) (*StorageContainerExporter, error) {
	labels := []string{"cluster_name", "container_name"}
	exporter := &StorageContainerExporter{
		Exporter: NewExporter(clusterName, api, "/v2.0/storage_containers/", labels),
	}
	if err := exporter.initMetrics(configPath, labels); err != nil {
		return nil, err
	}
	return exporter, nil
}

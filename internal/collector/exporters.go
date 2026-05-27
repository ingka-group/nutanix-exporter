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

package collector

import (
	"github.com/ingka-group/nutanix-exporter/internal/nutanix"
)

func NewClusterCollector(clusterName string, api nutanix.NutanixClient, configPath string) (*Exporter, error) {
	labels := []string{"cluster_name"}
	e := NewExporter(clusterName, api, "/v2.0/cluster/", labels)
	if err := e.initMetricsFromFile(configPath, labels); err != nil {
		return nil, err
	}
	return e, nil
}

func NewHostCollector(clusterName string, api nutanix.NutanixClient, configPath string) (*Exporter, error) {
	labels := []string{"cluster_name", "host_name"}
	e := NewExporter(clusterName, api, "/v2.0/hosts/", labels)
	if err := e.initMetricsFromFile(configPath, labels); err != nil {
		return nil, err
	}
	return e, nil
}

func NewVMCollector(clusterName string, api nutanix.NutanixClient, configPath string) (*Exporter, error) {
	labels := []string{"cluster_name", "vm_name"}
	e := NewExporter(clusterName, api, "/v2.0/vms/", labels)
	if err := e.initMetricsFromFile(configPath, labels); err != nil {
		return nil, err
	}
	return e, nil
}

func NewVMv1Collector(clusterName string, api nutanix.NutanixClient, configPath string) (*Exporter, error) {
	labels := []string{"cluster_name", "vm_name"}
	e := NewExporter(clusterName, api, "/v1/vms/", labels)
	if err := e.initMetricsFromFile(configPath, labels); err != nil {
		return nil, err
	}
	return e, nil
}

func NewStorageContainerCollector(clusterName string, api nutanix.NutanixClient, configPath string) (*Exporter, error) {
	labels := []string{"cluster_name", "container_name"}
	e := NewExporter(clusterName, api, "/v2.0/storage_containers/", labels)
	if err := e.initMetricsFromFile(configPath, labels); err != nil {
		return nil, err
	}
	return e, nil
}

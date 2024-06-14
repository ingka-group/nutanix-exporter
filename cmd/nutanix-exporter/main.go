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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"nutanix-exporter/internal/auth"
	"nutanix-exporter/internal/nutanix"
	"nutanix-exporter/internal/prom"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	ListenAddress  = ":9408"
	DefaultSection = "default"
)

var (
	VaultClient *auth.VaultClient
	ClustersMap map[string]*nutanix.Cluster
)

// main is the entrypoint of the exporter
func main() {
	log.Printf("Initializing Vault client")
	vaultClient, err := auth.NewVaultClient()
	if err != nil {
		log.Fatalf("Failed to create Vault client: %v", err)
	}

	PCClusterName := os.Getenv("PC_CLUSTER_NAME")
	if PCClusterName == "" {
		log.Fatalf("PC_CLUSTER_NAME environment variable is not set")
	}

	PCClusterURL := os.Getenv("PC_CLUSTER_URL")
	if PCClusterURL == "" {
		log.Fatalf("PC_CLUSTER_URL environment variable is not set")
	}

	log.Printf("Connecting to Prism Central")
	PCCluster := nutanix.NewCluster(PCClusterName, PCClusterURL, vaultClient, true, true, 10*time.Second)
	if PCCluster == nil {
		log.Fatalf("Failed to connect to Prism Central cluster")
	}

	log.Printf("Initializing clusters")
	clusterMap, err := InitClusters(PCCluster, vaultClient)
	if err != nil {
		log.Fatalf("Failed to initialize clusters: %v", err)
	}

	log.Printf("Initializing HTTP server")
	setupMetricServer(clusterMap, vaultClient)
}

// setupMetricServer starts the HTTP server and registers the metrics and index handlers
func setupMetricServer(clusterMap map[string]*nutanix.Cluster, vaultClient *auth.VaultClient) {
	// Register a handler for the root and a catch-all metrics handler
	http.HandleFunc("/", indexHandler)

	// Dynamically create routes for each cluster
	for name, cluster := range clusterMap {
		route := fmt.Sprintf("/metrics/%s", name)
		http.HandleFunc(route, createClusterMetricsHandler(cluster, vaultClient))
		log.Printf("Registered metrics endpoint for cluster %s at %s", name, route)
	}

	log.Printf("Starting Server on %s", ListenAddress)
	if err := http.ListenAndServe(ListenAddress, nil); err != nil {
		log.Fatalf("Error starting server: %s", err)
	}
}

// createClusterMetricsHandler returns a http.HandlerFunc that serves metrics for a specific cluster
func createClusterMetricsHandler(cluster *nutanix.Cluster, vaultClient *auth.VaultClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Refresh credentials for the specific cluster
		cluster.RefreshCredentialsIfNeeded(vaultClient)

		// Serve metrics from the specific cluster's registry
		promhttp.HandlerFor(cluster.Registry, promhttp.HandlerOpts{}).ServeHTTP(w, r)
	}
}

// indexHandler handles the / endpoint
func indexHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, `<html><head><title>Nutanix Exporter</title></head><body><h1>Nutanix Exporter</h1><p><a href="/metrics">Metrics</a></p></body></html>`)
}

// FetchClusterData fetches the name and IP of all Prism Element clusters registered in Prism Central.
func FetchClusterData(prismClient *nutanix.Cluster) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clusterData := make(map[string]string)
	resp, err := prismClient.API.MakeRequest(ctx, "GET", "/api/clustermgmt/v4.0.b1/config/clusters")
	if err != nil {
		return nil, err // Return the error to be handled by the caller
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Loop through result.data and extract name and IP of each cluster
	for _, cluster := range result["data"].([]interface{}) {
		name := cluster.(map[string]interface{})["name"].(string)
		ip := cluster.(map[string]interface{})["network"].(map[string]interface{})["externalAddress"].(map[string]interface{})["ipv4"].(map[string]interface{})["value"].(string)

		// only add clusters who's name starts with "DS"
		//if name[:2] != "DS" {
		//	continue
		//}

		clusterData[name] = fmt.Sprintf("https://%s:9440", ip)
		log.Printf("Found cluster %s at %s", name, clusterData[name])
	}

	return clusterData, nil
}

// InitClusters fetches all Prism Element clusters registered in Prism Central and creates Prometheus collectors for each.
func InitClusters(prismClient *nutanix.Cluster, vaultClient *auth.VaultClient) (map[string]*nutanix.Cluster, error) {
	clusterData, err := FetchClusterData(prismClient)
	if err != nil {
		return nil, err // Propagate the error up
	}

	clustersMap := make(map[string]*nutanix.Cluster)
	for name, url := range clusterData {
		cluster := nutanix.NewCluster(name, url, vaultClient, false, true, 10*time.Second)
		if cluster == nil {
			log.Printf("Failed to initialize cluster %s", name)
			continue
		}

		// Register collectors for this cluster
		log.Printf("Registering collectors for cluster %s", name)
		collectors := []prometheus.Collector{
			prom.NewStorageContainerCollector(cluster, "configs/storage_container.yaml"),
			prom.NewClusterCollector(cluster, "configs/cluster.yaml"),
			prom.NewHostCollector(cluster, "configs/host.yaml"),
			prom.NewVMCollector(cluster, "configs/vm.yaml"),
		}

		for _, collector := range collectors {
			cluster.Registry.MustRegister(collector)
		}
		cluster.Collectors = collectors

		// Add the cluster to the map
		clustersMap[name] = cluster
	}

	return clustersMap, nil
}

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

package exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ingka-group/nutanix-exporter/internal/auth"
	"github.com/ingka-group/nutanix-exporter/internal/nutanix"
	"github.com/ingka-group/nutanix-exporter/internal/prom"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	ListenAddress  = ":9408"
	DefaultSection = "default"
)

var (
	ClusterPrefix string
	VaultClient   *auth.VaultClient
	ClustersMap   map[string]*nutanix.Cluster
)

func Init() {

	// Get environment variables
	PCClusterName := getEnvOrFatal("PC_CLUSTER_NAME")
	PCClusterURL := getEnvOrFatal("PC_CLUSTER_URL")
	ClusterPrefix = os.Getenv("CLUSTER_PREFIX") // Optional

	log.Printf("Initializing Vault client")
	vaultClient, err := auth.NewVaultClient()
	if err != nil {
		log.Fatalf("Failed to create Vault client: %v", err)
	}

	log.Printf("Connecting to Prism Central")
	PCCluster := nutanix.NewCluster(PCClusterName, PCClusterURL, vaultClient, true, true, 10*time.Second)
	if PCCluster == nil {
		log.Fatalf("Failed to connect to Prism Central cluster")
	}

	log.Printf("Initializing clusters")
	clusterMap, err := SetupClusters(PCCluster, vaultClient)
	if err != nil {
		log.Fatalf("Failed to initialize clusters: %v", err)
	}

	log.Printf("Initializing HTTP server")
	http.HandleFunc("/", indexHandler)

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

// SetupClusters creates Prometheus collectors for every cluster registered in Prism Central
func SetupClusters(prismClient *nutanix.Cluster, vaultClient *auth.VaultClient) (map[string]*nutanix.Cluster, error) {
	clusterData, err := FetchClusters(prismClient)
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

// FetchClusters fetches the name and IP of all Prism Element clusters registered in Prism Central.
// Skips clusters that don't match the prefix if provided. (Env variable CLUSTER_PREFIX)
func FetchClusters(prismClient *nutanix.Cluster) (map[string]string, error) {
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

		// Skip the unnamed cluster
		if name == "Unnamed" {
			continue
		}

		// Skip clusters that don't match the prefix if provided
		if ClusterPrefix != "" && !strings.HasPrefix(name, ClusterPrefix) {
			log.Printf("Skipping cluster %s", name)
			continue
		}

		clusterData[name] = fmt.Sprintf("https://%s:9440", ip)
		log.Printf("Found cluster %s at %s", name, clusterData[name])
	}

	return clusterData, nil
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

// getEnvOrFatal returns the value of the specified environment variable or exits
func getEnvOrFatal(envVar string) string {
	value := os.Getenv(envVar)
	if value == "" {
		log.Fatalf("%s environment variable is not set", envVar)
	}
	return value
}

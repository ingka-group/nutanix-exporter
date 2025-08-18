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

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ingka-group/nutanix-exporter/internal/auth"
	"github.com/ingka-group/nutanix-exporter/internal/config"
	"github.com/ingka-group/nutanix-exporter/internal/nutanix"
	"github.com/ingka-group/nutanix-exporter/internal/prom"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const ListenAddress = ":9408"

type ExporterService struct {
	config             *config.Config
	credentialProvider auth.CredentialProvider
	clustersMap        map[string]*nutanix.Cluster
	clustersMu         sync.RWMutex
	server             *http.Server
	pcCluster          *nutanix.Cluster
}

func NewExporterService(cfg *config.Config, credProvider auth.CredentialProvider) *ExporterService {
	return &ExporterService{
		config:             cfg,
		credentialProvider: credProvider,
		clustersMap:        make(map[string]*nutanix.Cluster),
	}
}

func (es *ExporterService) Start() error {
	// Initialize Prism Central connection
	if err := es.initializePrismCentral(); err != nil {
		return fmt.Errorf("failed to initialize Prism Central: %w", err)
	}

	// Initialize clusters
	if err := es.refreshClusters(); err != nil {
		return fmt.Errorf("failed to initialize clusters: %w", err)
	}

	// Start refresh goroutines
	es.startRefreshRoutines()

	// Setup HTTP server
	es.setupHTTPHandlers()

	// Start server
	go func() {
		slog.Info("Starting server", "address", ListenAddress)
		if err := es.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
		}
	}()

	return nil
}

func (es *ExporterService) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if es.server != nil {
		return es.server.Shutdown(ctx)
	}
	return nil
}

func (es *ExporterService) initializePrismCentral() error {
	slog.Info("Connecting to Prism Central", "url", es.config.PrismCentralURL, "name", es.config.PrismCentralName)

	es.pcCluster = nutanix.NewCluster(
		es.config.PrismCentralName,
		es.config.PrismCentralURL,
		es.credentialProvider,
		true, // isPC
		true, // skipTLSVerify
		10*time.Second,
	)

	if es.pcCluster == nil {
		return fmt.Errorf("failed to connect to Prism Central cluster")
	}

	return nil
}

func (es *ExporterService) startRefreshRoutines() {
	// Credential refresh
	if es.config.VaultRefreshInterval > 0 {
		go func() {
			ticker := time.NewTicker(es.config.VaultRefreshInterval)
			defer ticker.Stop()
			for range ticker.C {
				slog.Info("Refreshing credentials...")
				if err := es.credentialProvider.Refresh(); err != nil {
					slog.Error("Failed to refresh credentials", "error", err)
				} else {
					slog.Info("Credentials refreshed successfully")
				}
			}
		}()
	}

	// Cluster refresh
	if es.config.ClusterRefreshInterval > 0 {
		go func() {
			ticker := time.NewTicker(es.config.ClusterRefreshInterval)
			defer ticker.Stop()
			for range ticker.C {
				slog.Info("Refreshing cluster list...")
				if err := es.refreshClusters(); err != nil {
					slog.Error("Failed to refresh clusters", "error", err)
				} else {
					slog.Info("Cluster list refreshed successfully")
				}
			}
		}()
	}
}

func (es *ExporterService) refreshClusters() error {
	clusterData, err := es.fetchClusters()
	if err != nil {
		return fmt.Errorf("failed to fetch clusters: %w", err)
	}

	newClustersMap := make(map[string]*nutanix.Cluster)
	for name, url := range clusterData {
		cluster := nutanix.NewCluster(
			name,
			url,
			es.credentialProvider,
			false, // isPC
			true,  // skipTLSVerify
			10*time.Second,
		)

		if cluster == nil {
			slog.Warn("Failed to initialize cluster", "name", name)
			continue
		}

		// Register collectors for this cluster
		slog.Info("Registering collectors for cluster", "name", name)
		collectors := []prometheus.Collector{
			prom.NewStorageContainerCollector(cluster, es.config.ConfigPath+"/storage_container.yaml"),
			prom.NewClusterCollector(cluster, es.config.ConfigPath+"/cluster.yaml"),
			prom.NewHostCollector(cluster, es.config.ConfigPath+"/host.yaml"),
			prom.NewVMCollector(cluster, es.config.ConfigPath+"/vm.yaml"),
		}

		for _, collector := range collectors {
			cluster.Registry.MustRegister(collector)
		}
		cluster.Collectors = collectors

		newClustersMap[name] = cluster
	}

	// Update the clusters map atomically
	es.clustersMu.Lock()
	es.clustersMap = newClustersMap
	es.clustersMu.Unlock()

	slog.Info("Clusters refreshed", "count", len(newClustersMap))
	return nil
}

func (es *ExporterService) fetchClusters() (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	clusterData := make(map[string]string)

	// Determine API version
	apiVersion := es.config.PCAPIVersion

	slog.Info("Fetching clusters", "api_version", apiVersion)

	// Select appropriate request and parse functions based on API version
	var makeRequest func(context.Context, int) (*http.Response, error)
	var parseClusters func(map[string]interface{}) ([]map[string]string, int, error)

	switch apiVersion {
	case "v3":
		makeRequest = es.makeV3Request
		parseClusters = es.parseV3Clusters
	case "v4b1":
		makeRequest = es.makeV4b1Request
		parseClusters = es.parseV4Clusters
	default: // v4
		makeRequest = es.makeV4Request
		parseClusters = es.parseV4Clusters
	}

	// Paginate through all results
	page := 0
	totalExpected := 0
	totalFetched := 0

	for {
		slog.Info("Fetching clusters page", "page", page)

		resp, err := makeRequest(ctx, page)
		if err != nil {
			return nil, fmt.Errorf("failed to make API request for page %d: %w", page, err)
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode response for page %d: %w", page, err)
		}
		resp.Body.Close()

		clusters, total, err := parseClusters(result)
		if err != nil {
			return nil, fmt.Errorf("failed to parse clusters for page %d: %w", page, err)
		}

		// Set total expected on first page
		if page == 0 {
			totalExpected = total
			slog.Info("Total clusters available", "total", totalExpected)
		}

		// Process clusters from this page
		pageClusterCount := 0
		duplicateCount := 0
		for _, cluster := range clusters {
			name := cluster["name"]
			ip := cluster["ip"]

			// Skip clusters that don't match the prefix if provided
			if es.config.ClusterPrefix != "" && !strings.HasPrefix(name, es.config.ClusterPrefix) {
				slog.Info("Skipping cluster due to prefix filter", "name", name, "prefix", es.config.ClusterPrefix)
				continue
			}

			// Check if we've already seen this cluster (handles duplicate results)
			if _, exists := clusterData[name]; exists {
				duplicateCount++
				slog.Info("Skipping duplicate cluster", "name", name)
				continue
			}

			clusterData[name] = fmt.Sprintf("https://%s:9440", ip)
			slog.Info("Found cluster", "name", name, "url", clusterData[name])
			pageClusterCount++
		}

		totalFetched += len(clusters)

		slog.Info("Processed clusters from page",
			"page", page,
			"clusters_on_page", len(clusters),
			"new_clusters", pageClusterCount,
			"duplicates", duplicateCount,
			"total_fetched", totalFetched,
			"total_unique", len(clusterData))

		// If all clusters on this page were duplicates, we're done
		if duplicateCount == len(clusters) && len(clusters) > 0 {
			slog.Info("All clusters on page were duplicates, stopping pagination", "page", page)
			break
		}

		// Check if we've fetched enough pages based on the limit
		// For v4 API: if we got less than 100 results, this is the last page
		if len(clusters) < 100 {
			slog.Info("Received partial page, stopping pagination",
				"page", page,
				"clusters_on_page", len(clusters))
			break
		}

		// Move to next page
		page++

		// Safety check to prevent infinite loops
		if page > 49 {
			slog.Warn("Reached maximum page limit, stopping pagination", "max_pages", 50)
			break
		}
	}

	slog.Info("Completed fetching all clusters",
		"total_api_reported", totalExpected,
		"total_clusters_fetched", totalFetched,
		"total_unique_clusters", len(clusterData))
	return clusterData, nil
}

// API request methods
func (es *ExporterService) makeV3Request(ctx context.Context, page int) (*http.Response, error) {
	payload := map[string]interface{}{
		"kind":   "cluster",
		"length": 100,
		"offset": page * 100,
	}
	return es.pcCluster.API.MakeRequestWithParams(ctx, "POST", "/api/nutanix/v3/clusters/list", nutanix.RequestParams{
		Payload: payload,
	})
}

func (es *ExporterService) makeV4Request(ctx context.Context, page int) (*http.Response, error) {
	return es.pcCluster.API.MakeRequestWithParams(ctx, "GET", "/api/clustermgmt/v4.0/config/clusters", nutanix.RequestParams{
		Params: url.Values{
			"$limit":   []string{"100"},
			"$page":    []string{fmt.Sprintf("%d", page)},
			"$orderby": []string{"name"},
		},
	})
}

func (es *ExporterService) makeV4b1Request(ctx context.Context, page int) (*http.Response, error) {
	return es.pcCluster.API.MakeRequestWithParams(ctx, "GET", "/api/clustermgmt/v4.0.b1/config/clusters", nutanix.RequestParams{
		Params: url.Values{
			"$limit":   []string{"100"},
			"$page":    []string{fmt.Sprintf("%d", page)},
			"$orderby": []string{"name"},
		},
	})
}

// Parsing methods with metadata extraction
func (es *ExporterService) parseV3Clusters(result map[string]interface{}) ([]map[string]string, int, error) {
	entities, ok := result["entities"].([]interface{})
	if !ok {
		return nil, 0, fmt.Errorf("unexpected v3 response format: missing 'entities' field")
	}

	// Extract total count from metadata
	metadata := result["metadata"].(map[string]interface{})
	totalCount := int(metadata["total_matches"].(float64))

	var clusters []map[string]string
	unnamedCount := 0
	for _, entity := range entities {
		cluster := entity.(map[string]interface{})
		spec := cluster["spec"].(map[string]interface{})
		status := cluster["status"].(map[string]interface{})

		name, ok := spec["name"].(string)
		if !ok || name == "" || name == "Unnamed" {
			unnamedCount++
			continue
		}

		resources := status["resources"].(map[string]interface{})
		network := resources["network"].(map[string]interface{})
		ip, ok := network["external_ip"].(string)
		if !ok || ip == "" {
			continue
		}

		clusters = append(clusters, map[string]string{
			"name": name,
			"ip":   ip,
		})
	}

	// Adjust total count to exclude unnamed clusters
	return clusters, totalCount - unnamedCount, nil
}

func (es *ExporterService) parseV4Clusters(result map[string]interface{}) ([]map[string]string, int, error) {
	data, ok := result["data"].([]interface{})
	if !ok {
		return nil, 0, fmt.Errorf("unexpected v4 response format: missing 'data' field")
	}

	// Extract total count from metadata
	metadata := result["metadata"].(map[string]interface{})
	totalCount := int(metadata["totalAvailableResults"].(float64))

	var clusters []map[string]string
	unnamedCount := 0
	for _, item := range data {
		clusterMap := item.(map[string]interface{})

		name, ok := clusterMap["name"].(string)
		if !ok || name == "" || name == "Unnamed" {
			unnamedCount++
			continue
		}

		// Navigate to network.externalAddress.ipv4.value
		network, networkOk := clusterMap["network"].(map[string]interface{})
		if !networkOk {
			continue
		}

		externalAddress, extOk := network["externalAddress"].(map[string]interface{})
		if !extOk {
			continue
		}

		ipv4, ipv4Ok := externalAddress["ipv4"].(map[string]interface{})
		if !ipv4Ok {
			continue
		}

		ip, ok := ipv4["value"].(string)
		if !ok || ip == "" {
			continue
		}

		clusters = append(clusters, map[string]string{
			"name": name,
			"ip":   ip,
		})
	}

	// Adjust total count to exclude unnamed clusters
	return clusters, totalCount - unnamedCount, nil
}

func (es *ExporterService) setupHTTPHandlers() {
	mux := http.NewServeMux()

	mux.HandleFunc("/", es.indexHandler)
	mux.HandleFunc("/metrics/", es.metricsHandler)

	es.server = &http.Server{
		Addr:    ListenAddress,
		Handler: mux,
	}
}

func (es *ExporterService) indexHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, `<html><head><title>Nutanix Exporter</title></head><body><h1>Nutanix Exporter</h1><p><a href="/metrics">Metrics</a></p></body></html>`)
}

func (es *ExporterService) metricsHandler(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/metrics/")

	es.clustersMu.RLock()
	cluster, ok := es.clustersMap[name]
	es.clustersMu.RUnlock()

	if !ok {
		http.NotFound(w, r)
		return
	}

	// Refresh credentials for the specific cluster
	cluster.RefreshCredentialsIfNeeded(es.credentialProvider)

	// Serve metrics from the specific cluster's registry
	promhttp.HandlerFor(cluster.Registry, promhttp.HandlerOpts{}).ServeHTTP(w, r)
}

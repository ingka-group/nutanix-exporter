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
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ingka-group/nutanix-exporter/internal/auth"
	"github.com/ingka-group/nutanix-exporter/internal/collector"
	"github.com/ingka-group/nutanix-exporter/internal/config"
	"github.com/ingka-group/nutanix-exporter/internal/nutanix"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// clusterEntry pairs a Nutanix cluster with its dedicated Prometheus registry.
// Keeping the registry here rather than on nutanix.Cluster means the HTTP client
// layer has no Prometheus dependency.
type clusterEntry struct {
	cluster  *nutanix.Cluster
	registry *prometheus.Registry
}

type ExporterService struct {
	config             *config.Config
	credentialProvider auth.CredentialProvider
	clustersMap        map[string]*clusterEntry
	clustersMu         sync.RWMutex
	server             *http.Server
	pcCluster          *nutanix.Cluster
}

func NewExporterService(cfg *config.Config, credProvider auth.CredentialProvider) *ExporterService {
	return &ExporterService{
		config:             cfg,
		credentialProvider: credProvider,
		clustersMap:        make(map[string]*clusterEntry),
	}
}

func (es *ExporterService) Start(ctx context.Context) error {
	return es.StartWithServer(ctx, true)
}

func (es *ExporterService) StartWithServer(ctx context.Context, startHTTPServer bool) error {
	// Initialize Prism Central connection
	if err := es.initializePrismCentral(); err != nil {
		return fmt.Errorf("failed to initialize Prism Central: %w", err)
	}

	// Initialize clusters
	if err := es.refreshClusters(ctx); err != nil {
		return fmt.Errorf("failed to initialize clusters: %w", err)
	}

	// Start refresh goroutines
	es.startRefreshRoutines(ctx)

	if startHTTPServer {
		// Setup HTTP server
		es.setupHTTPHandlers()

		// Start server
		go func() {
			slog.Info("Starting server", "address", es.config.ListenAddress)
			if err := es.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("Server error", "error", err)
			}
		}()
	}

	return nil
}

func (es *ExporterService) GetHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		es.clustersMu.RLock()
		gatherers := make(prometheus.Gatherers, 0, len(es.clustersMap))
		for _, cluster := range es.clustersMap {
			gatherers = append(gatherers, cluster.registry)
		}
		es.clustersMu.RUnlock()
		promhttp.HandlerFor(gatherers, promhttp.HandlerOpts{}).ServeHTTP(w, r)
	})
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

func (es *ExporterService) startRefreshRoutines(ctx context.Context) {
	// Credential refresh
	if es.config.VaultRefreshInterval > 0 {
		go func() {
			ticker := time.NewTicker(es.config.VaultRefreshInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					slog.Info("Refreshing credentials...")
					if err := es.credentialProvider.Refresh(); err != nil {
						slog.Error("Failed to refresh credentials", "error", err)
					} else {
						slog.Info("Credentials refreshed successfully")
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Cluster refresh
	if es.config.ClusterRefreshInterval > 0 {
		go func() {
			ticker := time.NewTicker(es.config.ClusterRefreshInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					slog.Info("Refreshing cluster list...")
					if err := es.refreshClusters(ctx); err != nil {
						slog.Error("Failed to refresh clusters", "error", err)
					} else {
						slog.Info("Cluster list refreshed successfully")
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}
}

func (es *ExporterService) refreshClusters(ctx context.Context) error {
	clusterData, err := nutanix.FetchClusters(ctx, es.pcCluster.API, es.config.PCAPIVersion, es.config.ClusterPrefix)
	if err != nil {
		return fmt.Errorf("failed to fetch clusters: %w", err)
	}

	newClustersMap := make(map[string]*clusterEntry)
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

		registry := prometheus.NewRegistry()

		slog.Info("Registering collectors for cluster", "name", name)
		scCollector, err := collector.NewStorageContainerCollector(cluster.Name, cluster.API, es.config.ConfigPath+"/storage_container.yaml")
		if err != nil {
			slog.Error("Failed to init storage container collector", "cluster", name, "error", err)
			continue
		}
		clusterCollector, err := collector.NewClusterCollector(cluster.Name, cluster.API, es.config.ConfigPath+"/cluster.yaml")
		if err != nil {
			slog.Error("Failed to init cluster collector", "cluster", name, "error", err)
			continue
		}
		hostCollector, err := collector.NewHostCollector(cluster.Name, cluster.API, es.config.ConfigPath+"/host.yaml")
		if err != nil {
			slog.Error("Failed to init host collector", "cluster", name, "error", err)
			continue
		}
		vmCollector, err := collector.NewVMCollector(cluster.Name, cluster.API, es.config.ConfigPath+"/vm.yaml")
		if err != nil {
			slog.Error("Failed to init VM collector", "cluster", name, "error", err)
			continue
		}
		vmv1Collector, err := collector.NewVMv1Collector(cluster.Name, cluster.API, es.config.ConfigPath+"/vm_v1.yaml")
		if err != nil {
			slog.Error("Failed to init VM v1 collector", "cluster", name, "error", err)
			continue
		}
		for _, collector := range []prometheus.Collector{scCollector, clusterCollector, hostCollector, vmCollector, vmv1Collector} {
			registry.MustRegister(collector)
		}

		newClustersMap[name] = &clusterEntry{cluster: cluster, registry: registry}
	}

	// Update the clusters map atomically
	es.clustersMu.Lock()
	es.clustersMap = newClustersMap
	es.clustersMu.Unlock()

	slog.Info("Clusters refreshed", "count", len(newClustersMap))
	return nil
}

func (es *ExporterService) setupHTTPHandlers() {
	mux := http.NewServeMux()

	mux.HandleFunc("/", es.indexHandler)
	mux.HandleFunc("/metrics/", es.metricsHandler)

	es.server = &http.Server{
		Addr:         es.config.ListenAddress,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
}

func (es *ExporterService) indexHandler(w http.ResponseWriter, r *http.Request) {
	_, _ = fmt.Fprint(w, `<html><head><title>Nutanix Exporter</title></head><body><h1>Nutanix Exporter</h1><p><a href="/metrics">Metrics</a></p></body></html>`)
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

	// Serve metrics from the specific cluster's registry
	promhttp.HandlerFor(cluster.registry, promhttp.HandlerOpts{}).ServeHTTP(w, r)
}

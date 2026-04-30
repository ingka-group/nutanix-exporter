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

package nutanix

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ingka-group/nutanix-exporter/internal/auth"
)

// Cluster represents a Nutanix cluster (Prism Central OR Element).
type Cluster struct {
	Name string
	URL  string `yaml:"URL"`
	API  NutanixClient
}

// NewCluster returns a new Nutanix Cluster, fetching credentials and constructing the API client.
// Returns nil if credentials cannot be obtained.
func NewCluster(name, rawURL string, ncp auth.CredentialProvider, isPC bool, skipTLSVerify bool, timeout time.Duration) *Cluster {
	var username, password string
	var err error

	if isPC {
		username, password, err = ncp.GetPCCreds(name)
	} else {
		username, password, err = ncp.GetPECreds(name)
	}

	if username == "" || password == "" {
		kind := "Prism Element"
		if isPC {
			kind = "Prism Central"
		}
		slog.Error("Failed to get credentials", "kind", kind, "name", name, "error", err)
		return nil
	}

	api := newClient(name, rawURL, username, password, ncp, isPC, skipTLSVerify, timeout)

	return &Cluster{
		Name: name,
		URL:  rawURL,
		API:  api,
	}
}

// FetchClusters queries Prism Central via client and returns a map of cluster name -> URL.
// apiVersion selects the PC API variant ("v3", "v4b1", or "v4" / anything else).
// prefix, when non-empty, filters clusters whose names don't start with it.
func FetchClusters(ctx context.Context, client NutanixClient, apiVersion, prefix string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var makeRequest func(context.Context, int) (*http.Response, error)
	var parseClusters func(map[string]any) ([]map[string]string, int, error)

	switch apiVersion {
	case "v3":
		makeRequest = func(ctx context.Context, page int) (*http.Response, error) {
			return fetchClustersV3(ctx, client, page)
		}
		parseClusters = parseClustersV3
	case "v4b1":
		makeRequest = func(ctx context.Context, page int) (*http.Response, error) {
			return fetchClustersV4b1(ctx, client, page)
		}
		parseClusters = parseClustersV4
	default: // v4
		makeRequest = func(ctx context.Context, page int) (*http.Response, error) {
			return fetchClustersV4(ctx, client, page)
		}
		parseClusters = parseClustersV4
	}

	clusterData := make(map[string]string)
	page := 0
	totalExpected := 0
	totalFetched := 0

	slog.Info("Fetching clusters", "api_version", apiVersion)

	for {
		slog.Info("Fetching clusters page", "page", page)

		resp, err := makeRequest(ctx, page)
		if err != nil {
			return nil, fmt.Errorf("failed to make API request for page %d: %w", page, err)
		}

		var result map[string]any
		decodeErr := func() (err error) {
			defer func() {
				if cerr := resp.Body.Close(); cerr != nil && err == nil {
					err = cerr
				}
			}()
			return json.NewDecoder(resp.Body).Decode(&result)
		}()
		if decodeErr != nil {
			return nil, fmt.Errorf("failed to decode response for page %d: %w", page, decodeErr)
		}

		clusters, total, err := parseClusters(result)
		if err != nil {
			return nil, fmt.Errorf("failed to parse clusters for page %d: %w", page, err)
		}

		if page == 0 {
			totalExpected = total
			slog.Info("Total clusters available", "total", totalExpected)
		}

		pageClusterCount := 0
		duplicateCount := 0
		for _, cluster := range clusters {
			name := cluster["name"]
			ip := cluster["ip"]

			if prefix != "" && !strings.HasPrefix(name, prefix) {
				slog.Info("Skipping cluster due to prefix filter", "name", name, "prefix", prefix)
				continue
			}

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

		if duplicateCount == len(clusters) && len(clusters) > 0 {
			slog.Info("All clusters on page were duplicates, stopping pagination", "page", page)
			break
		}

		if len(clusters) < 100 {
			slog.Info("Received partial page, stopping pagination",
				"page", page,
				"clusters_on_page", len(clusters))
			break
		}

		page++

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

func fetchClustersV3(ctx context.Context, client NutanixClient, page int) (*http.Response, error) {
	return client.MakeRequest(ctx, "POST", "/api/nutanix/v3/clusters/list", RequestOptions{
		Payload: map[string]any{
			"kind":   "cluster",
			"length": 100,
			"offset": page * 100,
		},
	})
}

func fetchClustersV4(ctx context.Context, client NutanixClient, page int) (*http.Response, error) {
	return client.MakeRequest(ctx, "GET", "/api/clustermgmt/v4.0/config/clusters", RequestOptions{
		Params: url.Values{
			"$limit":   []string{"100"},
			"$page":    []string{fmt.Sprintf("%d", page)},
			"$orderby": []string{"name"},
		},
	})
}

func fetchClustersV4b1(ctx context.Context, client NutanixClient, page int) (*http.Response, error) {
	return client.MakeRequest(ctx, "GET", "/api/clustermgmt/v4.0.b1/config/clusters", RequestOptions{
		Params: url.Values{
			"$limit":   []string{"100"},
			"$page":    []string{fmt.Sprintf("%d", page)},
			"$orderby": []string{"name"},
		},
	})
}

func parseClustersV3(result map[string]any) ([]map[string]string, int, error) {
	entities, ok := result["entities"].([]any)
	if !ok {
		return nil, 0, fmt.Errorf("unexpected v3 response format: missing 'entities' field")
	}

	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		return nil, 0, fmt.Errorf("unexpected v3 response format: missing 'metadata' field")
	}
	totalMatches, ok := metadata["total_matches"].(float64)
	if !ok {
		return nil, 0, fmt.Errorf("unexpected v3 response format: missing 'total_matches' field")
	}
	totalCount := int(totalMatches)

	var clusters []map[string]string
	unnamedCount := 0
	for _, entity := range entities {
		cluster, ok := entity.(map[string]any)
		if !ok {
			continue
		}
		spec, ok := cluster["spec"].(map[string]any)
		if !ok {
			continue
		}
		status, ok := cluster["status"].(map[string]any)
		if !ok {
			continue
		}

		name, ok := spec["name"].(string)
		if !ok || name == "" || name == "Unnamed" {
			unnamedCount++
			continue
		}

		resources, ok := status["resources"].(map[string]any)
		if !ok {
			continue
		}
		network, ok := resources["network"].(map[string]any)
		if !ok {
			continue
		}
		ip, ok := network["external_ip"].(string)
		if !ok || ip == "" {
			continue
		}

		clusters = append(clusters, map[string]string{
			"name": name,
			"ip":   ip,
		})
	}

	return clusters, totalCount - unnamedCount, nil
}

func parseClustersV4(result map[string]any) ([]map[string]string, int, error) {
	data, ok := result["data"].([]any)
	if !ok {
		return nil, 0, fmt.Errorf("unexpected v4 response format: missing 'data' field")
	}

	metadata, ok := result["metadata"].(map[string]any)
	if !ok {
		return nil, 0, fmt.Errorf("unexpected v4 response format: missing 'metadata' field")
	}
	totalAvailable, ok := metadata["totalAvailableResults"].(float64)
	if !ok {
		return nil, 0, fmt.Errorf("unexpected v4 response format: missing 'totalAvailableResults' field")
	}
	totalCount := int(totalAvailable)

	var clusters []map[string]string
	unnamedCount := 0
	for _, item := range data {
		clusterMap, ok := item.(map[string]any)
		if !ok {
			continue
		}

		name, ok := clusterMap["name"].(string)
		if !ok || name == "" || name == "Unnamed" {
			unnamedCount++
			continue
		}

		network, networkOk := clusterMap["network"].(map[string]any)
		if !networkOk {
			continue
		}

		externalAddress, extOk := network["externalAddress"].(map[string]any)
		if !extOk {
			continue
		}

		ipv4, ipv4Ok := externalAddress["ipv4"].(map[string]any)
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

	return clusters, totalCount - unnamedCount, nil
}

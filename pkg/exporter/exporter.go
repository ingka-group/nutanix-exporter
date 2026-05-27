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

// Package exporter provides a public API for embedding the Nutanix exporter
// in other applications without starting its built-in HTTP server.
package exporter

import (
	"context"
	"net/http"
	"time"

	"github.com/ingka-group/nutanix-exporter/internal/auth"
	"github.com/ingka-group/nutanix-exporter/internal/config"
	"github.com/ingka-group/nutanix-exporter/internal/service"
	"github.com/prometheus/exporter-toolkit/web"
)

// Config holds configuration for the Nutanix exporter service.
type Config struct {
	PrismCentralURL        string
	PrismCentralName       string
	ClusterRefreshInterval time.Duration
	ClusterPrefix          string
	PCAPIVersion           string
	ConfigPath             string
}

// CredentialProvider defines the interface for Nutanix credential management.
type CredentialProvider = auth.CredentialProvider

// ExporterService wraps the internal exporter service.
type ExporterService struct {
	svc *service.ExporterService
}

// NewExporterService creates a new ExporterService with the given configuration
// and credential provider.
func NewExporterService(cfg *Config, credProvider CredentialProvider) *ExporterService {
	internalCfg := &config.Config{
		PrismCentralURL:        cfg.PrismCentralURL,
		PrismCentralName:       cfg.PrismCentralName,
		ClusterRefreshInterval: cfg.ClusterRefreshInterval,
		ClusterPrefix:          cfg.ClusterPrefix,
		PCAPIVersion:           cfg.PCAPIVersion,
		ConfigPath:             cfg.ConfigPath,
	}
	return &ExporterService{svc: service.NewExporterService(internalCfg, credProvider)}
}

// StartWithServer initializes the exporter. When webFlags is non-nil, the
// built-in HTTP server is started with TLS/auth support from exporter-toolkit.
// Pass nil to skip the HTTP server (useful when embedding the exporter).
func (es *ExporterService) StartWithServer(ctx context.Context, webFlags *web.FlagConfig) error {
	return es.svc.StartWithServer(ctx, webFlags)
}

// GetHandler returns an http.Handler that serves combined Prometheus metrics
// from all discovered Nutanix clusters.
func (es *ExporterService) GetHandler() http.Handler {
	return es.svc.GetHandler()
}

// Stop shuts down the exporter service gracefully.
func (es *ExporterService) Stop() error {
	return es.svc.Stop()
}

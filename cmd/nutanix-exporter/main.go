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

package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ingka-group/nutanix-exporter/internal/auth"
	"github.com/ingka-group/nutanix-exporter/internal/config"
	"github.com/ingka-group/nutanix-exporter/internal/service"
)

// main is the entrypoint of the exporter
func main() {

	// Set up global structured Logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Load configuration
	cfg, err := config.NewConfig()
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Create credential provider based on configuration
	var credProvider auth.CredentialProvider
	if cfg.VaultAddress != "" {
		credProvider, err = auth.NewVaultCredentialProvider(cfg)
		if err != nil {
			slog.Error("Failed to create vault credential provider", "error", err)
			os.Exit(1)
		}
		slog.Info("Using Vault credential provider")
	} else {
		credProvider = auth.NewEnvCredentialProvider()
		slog.Info("Using environment variable credential provider")
	}

	// Create and start exporter service
	exporterService := service.NewExporterService(cfg, credProvider)
	if err := exporterService.Start(); err != nil {
		slog.Error("Failed to start exporter service", "error", err)
		os.Exit(1)
	}

	// Wait for shutdown signal
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	defer stop()
	<-ctx.Done()

	// Stop services and disconnect clients
	stop()
	exporterService.Stop()
	slog.Info("Graceful shutdown completed")
}

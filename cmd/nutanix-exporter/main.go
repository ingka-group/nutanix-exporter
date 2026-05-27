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

package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kingpin/v2"
	"github.com/ingka-group/nutanix-exporter/internal/auth"
	"github.com/ingka-group/nutanix-exporter/internal/config"
	"github.com/ingka-group/nutanix-exporter/internal/service"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/prometheus/exporter-toolkit/web/kingpinflag"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.NewConfig()
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	// CLI flags (compatible with prometheus exporter-toolkit)
	webFlags := kingpinflag.AddFlags(kingpin.CommandLine, cfg.ListenAddress)
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	defer stop()

	if err = web.Validate(*webFlags.WebConfigFile); err != nil {
		slog.Error("Invalid web configuration", "error", err)
		os.Exit(1)
	}

	exporterService := service.NewExporterService(cfg, credProvider)
	if err = exporterService.Start(ctx, webFlags); err != nil {
		slog.Error("Failed to start exporter service", "error", err)
		os.Exit(1)
	}

	<-ctx.Done()

	stop()
	if err = exporterService.Stop(); err != nil {
		slog.Error("Failed to stop exporter service", "error", err)
	}
	slog.Info("Graceful shutdown completed")
}

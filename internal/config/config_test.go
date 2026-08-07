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

package config

import (
	"os"
	"testing"
)

func unsetenv(t *testing.T, key string) {
	t.Helper()
	prev, hadPrev := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("failed to unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if hadPrev {
			if err := os.Setenv(key, prev); err != nil {
				t.Errorf("failed to restore %s: %v", key, err)
			}
		} else {
			if err := os.Unsetenv(key); err != nil {
				t.Errorf("failed to unset %s: %v", key, err)
			}
		}
	})
}

func Test_NewConfig(t *testing.T) {
	t.Run("required vars set", func(t *testing.T) {
		t.Setenv("PC_CLUSTER_URL", "https://10.0.0.1:9440")
		t.Setenv("PC_CLUSTER_NAME", "my-pc")

		cfg, err := NewConfig()
		if err != nil {
			t.Fatalf("NewConfig() unexpected error: %v", err)
		}
		if cfg.PrismCentralURL != "https://10.0.0.1:9440" {
			t.Errorf("PrismCentralURL = %q, want %q", cfg.PrismCentralURL, "https://10.0.0.1:9440")
		}
		if cfg.PrismCentralName != "my-pc" {
			t.Errorf("PrismCentralName = %q, want %q", cfg.PrismCentralName, "my-pc")
		}
	})

	t.Run("defaults are applied", func(t *testing.T) {
		t.Setenv("PC_CLUSTER_URL", "https://10.0.0.1:9440")
		t.Setenv("PC_CLUSTER_NAME", "my-pc")
		t.Setenv("CLUSTER_REFRESH_INTERVAL", "")
		t.Setenv("PC_API_VERSION", "")
		t.Setenv("CONFIG_PATH", "")
		t.Setenv("VAULT_REFRESH_INTERVAL", "")

		cfg, err := NewConfig()
		if err != nil {
			t.Fatalf("NewConfig() unexpected error: %v", err)
		}
		if cfg.PCAPIVersion != "v4" {
			t.Errorf("PCAPIVersion = %q, want %q", cfg.PCAPIVersion, "v4")
		}
		if cfg.ConfigPath != "./configs" {
			t.Errorf("ConfigPath = %q, want %q", cfg.ConfigPath, "./configs")
		}
	})

	t.Run("SkipPCAppliance defaults to false when unset", func(t *testing.T) {
		t.Setenv("PC_CLUSTER_URL", "https://10.0.0.1:9440")
		t.Setenv("PC_CLUSTER_NAME", "my-pc")
		unsetenv(t, "SKIP_PC_APPLIANCE")

		cfg, err := NewConfig()
		if err != nil {
			t.Fatalf("NewConfig() unexpected error: %v", err)
		}
		if cfg.SkipPCAppliance {
			t.Error("SkipPCAppliance = true, want false so existing deployments keep scraping the PC appliance")
		}
	})

	t.Run("SkipPCAppliance is opt in", func(t *testing.T) {
		tests := []struct {
			value string
			want  bool
		}{
			{value: "true", want: true},
			{value: "1", want: true},
			{value: "false", want: false},
			{value: "0", want: false},
		}

		for _, tt := range tests {
			t.Run(tt.value, func(t *testing.T) {
				t.Setenv("PC_CLUSTER_URL", "https://10.0.0.1:9440")
				t.Setenv("PC_CLUSTER_NAME", "my-pc")
				t.Setenv("SKIP_PC_APPLIANCE", tt.value)

				cfg, err := NewConfig()
				if err != nil {
					t.Fatalf("NewConfig() unexpected error: %v", err)
				}
				if cfg.SkipPCAppliance != tt.want {
					t.Errorf("SKIP_PC_APPLIANCE=%q gave SkipPCAppliance = %v, want %v", tt.value, cfg.SkipPCAppliance, tt.want)
				}
			})
		}
	})

	t.Run("SDTargetAddress defaults to empty so the request Host is used", func(t *testing.T) {
		t.Setenv("PC_CLUSTER_URL", "https://10.0.0.1:9440")
		t.Setenv("PC_CLUSTER_NAME", "my-pc")
		unsetenv(t, "EXPORTER_SD_TARGET")

		cfg, err := NewConfig()
		if err != nil {
			t.Fatalf("NewConfig() unexpected error: %v", err)
		}
		if cfg.SDTargetAddress != "" {
			t.Errorf("SDTargetAddress = %q, want empty", cfg.SDTargetAddress)
		}
	})

	t.Run("SDTargetAddress is read from the environment", func(t *testing.T) {
		t.Setenv("PC_CLUSTER_URL", "https://10.0.0.1:9440")
		t.Setenv("PC_CLUSTER_NAME", "my-pc")
		t.Setenv("EXPORTER_SD_TARGET", "nutanix-exporter:9408")

		cfg, err := NewConfig()
		if err != nil {
			t.Fatalf("NewConfig() unexpected error: %v", err)
		}
		if cfg.SDTargetAddress != "nutanix-exporter:9408" {
			t.Errorf("SDTargetAddress = %q, want %q", cfg.SDTargetAddress, "nutanix-exporter:9408")
		}
	})

	t.Run("missing required var returns error", func(t *testing.T) {
		unsetenv(t, "PC_CLUSTER_URL")
		unsetenv(t, "PC_CLUSTER_NAME")

		_, err := NewConfig()
		if err == nil {
			t.Fatal("NewConfig() expected error for missing required vars, got nil")
		}
	})
}

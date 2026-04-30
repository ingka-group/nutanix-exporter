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

	t.Run("missing required var returns error", func(t *testing.T) {
		unsetenv(t, "PC_CLUSTER_URL")
		unsetenv(t, "PC_CLUSTER_NAME")

		_, err := NewConfig()
		if err == nil {
			t.Fatal("NewConfig() expected error for missing required vars, got nil")
		}
	})
}

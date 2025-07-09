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

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hashicorp/vault-client-go"
	"github.com/hashicorp/vault-client-go/schema"
	"github.com/ingka-group/nutanix-exporter/internal/config"
)

const Timeout = 30 * time.Second

type VaultCredentialProvider struct {
	client        *vault.Client
	config        *config.Config
	pcTaskAccount string
	peTaskAccount string
	engineName    string
}

// NewVaultCredentialProvider creates a new VaultCredentialProvider instance.
func NewVaultCredentialProvider(cfg *config.Config) (*VaultCredentialProvider, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()

	client, err := vault.New(
		vault.WithAddress(cfg.VaultAddress),
		vault.WithRequestTimeout(Timeout),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create vault client: %w", err)
	}

	// Authenticate
	loginRequest := schema.AppRoleLoginRequest{
		RoleId:   cfg.VaultRoleId,
		SecretId: cfg.VaultSecretId,
	}

	var resp *vault.Response[map[string]interface{}]
	if cfg.VaultNamespace != "" {
		resp, err = client.Auth.AppRoleLogin(ctx, loginRequest, vault.WithNamespace(cfg.VaultNamespace))
	} else {
		resp, err = client.Auth.AppRoleLogin(ctx, loginRequest)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to authenticate with vault: %w", err)
	}

	if err := client.SetToken(resp.Auth.ClientToken); err != nil {
		return nil, fmt.Errorf("failed to set vault token: %w", err)
	}

	if cfg.VaultNamespace != "" {
		if err = client.SetNamespace(cfg.VaultNamespace); err != nil {
			return nil, fmt.Errorf("failed to set vault namespace: %w", err)
		}
	}

	return &VaultCredentialProvider{
		client:        client,
		config:        cfg,
		pcTaskAccount: cfg.PCTaskAccount,
		peTaskAccount: cfg.PETaskAccount,
		engineName:    cfg.VaultEngineName,
	}, nil
}

// Refresh refreshes the Vault client by creating a new instance with the same configuration.
func (v *VaultCredentialProvider) Refresh() error {
	newProvider, err := NewVaultCredentialProvider(v.config)
	if err != nil {
		return fmt.Errorf("failed to refresh vault client: %w", err)
	}

	v.client = newProvider.client
	slog.Info("Vault client refreshed successfully")
	return nil
}

// GetPCCreds retrieves the credentials for a given cluster.
func (v *VaultCredentialProvider) GetPCCreds(cluster string) (string, string, error) {
	return v.getCreds(cluster, v.pcTaskAccount, v.engineName)
}

// GetPECreds retrieves the credentials for a given cluster.
func (v *VaultCredentialProvider) GetPECreds(cluster string) (string, string, error) {
	return v.getCreds(cluster, v.peTaskAccount, v.engineName)
}

// GetCreds retrieves the credentials for a given cluster, path, and engine.
func (v *VaultCredentialProvider) getCreds(cluster, path, engine string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()

	vaultResponse, err := v.client.Secrets.KvV2Read(ctx, fmt.Sprintf("%s/%s", cluster, path), vault.WithMountPath(engine))
	if err != nil {
		return "", "", fmt.Errorf("failed to read secret: %w", err)
	}

	jsonData, err := json.Marshal(vaultResponse.Data.Data)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal secret data: %w", err)
	}

	var vaultSecret struct {
		Username string `json:"username"`
		Secret   string `json:"secret"`
	}
	if err := json.Unmarshal(jsonData, &vaultSecret); err != nil {
		return "", "", fmt.Errorf("failed to parse secret data: %w", err)
	}

	return vaultSecret.Username, vaultSecret.Secret, nil
}

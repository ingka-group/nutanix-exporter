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
	"log"
	"os"
	"time"

	"github.com/hashicorp/vault-client-go"
	"github.com/hashicorp/vault-client-go/schema"
)

const (
	Timeout = 30 * time.Second
)

var (
	PCTaskAccount string
	PETaskAccount string
	EngineName    string
)

// VaultClient is a wrapper around the Vault client
type VaultClient struct {
	client *vault.Client
}

// getEnvOrFatal returns the value of the specified environment variable or exits the program
func getEnvOrFatal(envVar string) string {
	value := os.Getenv(envVar)
	if value == "" {
		log.Fatalf("%s environment variable is not set", envVar)
	}
	return value
}

// NewVaultClient creates a new Vault client and authenticates using AppRole
// Uses the VAULT_ADDR, VAULT_ROLE_ID, VAULT_SECRET_ID and VAULT_NAMESPACE environment variables
func NewVaultClient() (*VaultClient, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()

	addr := getEnvOrFatal("VAULT_ADDR")
	roleId := getEnvOrFatal("VAULT_ROLE_ID")
	secretId := getEnvOrFatal("VAULT_SECRET_ID")
	namespace := getEnvOrFatal("VAULT_NAMESPACE")
	PETaskAccount = getEnvOrFatal("PE_TASK_ACCOUNT")
	PCTaskAccount = getEnvOrFatal("PC_TASK_ACCOUNT")
	EngineName = getEnvOrFatal("VAULT_ENGINE_NAME")

	log.Printf("Creating new Vault client for %s", addr)
	client, err := vault.New(
		vault.WithAddress(addr),
		vault.WithRequestTimeout(Timeout),
	)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Authenticating with Vault using AppRole")
	resp, err := client.Auth.AppRoleLogin(
		ctx,
		schema.AppRoleLoginRequest{
			RoleId:   roleId,
			SecretId: secretId,
		},
		vault.WithNamespace(namespace),
	)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Setting token for Vault client")
	if err := client.SetToken(resp.Auth.ClientToken); err != nil {
		log.Fatal(err)
	}

	if err = client.SetNamespace(namespace); err != nil {
		log.Fatal(err)
	}

	return &VaultClient{client: client}, nil
}

// GetSecret reads a secret from Vault using KV V2 secrets engine
func (v *VaultClient) GetSecret(path, engine string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()

	// Read the secret from the specified path using KV V2
	vaultResponse, err := v.client.Secrets.KvV2Read(ctx, path, vault.WithMountPath(engine))
	if err != nil {
		return "", err
	}

	// Marshal the secret data into JSON
	jsonData, err := json.Marshal(vaultResponse.Data.Data)
	if err != nil {
		return "", fmt.Errorf("error marshalling secret data to JSON: %s", err)
	}

	return string(jsonData), nil
}

// GetPCCreds returns the username and password for the specified Prism Central cluster
func (v *VaultClient) GetPCCreds(cluster string) (string, string) {
	return v.GetCreds(cluster, PCTaskAccount, EngineName)
}

// GetPECreds returns the username and password for the specified Prism Element cluster
func (v *VaultClient) GetPECreds(cluster string) (string, string) {
	return v.GetCreds(cluster, PETaskAccount, EngineName)
}

// GetCreds returns the username and password for the specified cluster, path, and engine
func (v *VaultClient) GetCreds(cluster, path, engine string) (string, string) {
	secrets, err := v.GetSecret(fmt.Sprintf("%s/%s", cluster, path), engine)
	if err != nil {
		log.Fatalf("Failed to get secrets for %s: %v", cluster, err)
	}

	var vaultSecret struct {
		Username string `json:"username"`
		Secret   string `json:"secret"`
	}
	if err := json.Unmarshal([]byte(secrets), &vaultSecret); err != nil {
		log.Fatalf("Failed to parse secrets for %s: %v", cluster, err)
	}
	return vaultSecret.Username, vaultSecret.Secret
}

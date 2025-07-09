package config

import (
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	PrismCentralURL        string        `env:"PC_CLUSTER_URL,required"`
	PrismCentralName       string        `env:"PC_CLUSTER_NAME,required"`
	ClusterRefreshInterval time.Duration `env:"CLUSTER_REFRESH_INTERVAL" envDefault:"30m"`
	ClusterPrefix          string        `env:"CLUSTER_PREFIX" envDefault:""`
	PCAPIVersion           string        `env:"PC_API_VERSION" envDefault:"v4"`
	VaultAddress           string        `env:"VAULT_ADDR"`
	VaultRoleId            string        `env:"VAULT_ROLE_ID"`
	VaultSecretId          string        `env:"VAULT_SECRET_ID"`
	VaultEngineName        string        `env:"VAULT_ENGINE_NAME"`
	VaultNamespace         string        `env:"VAULT_NAMESPACE"`
	VaultRefreshInterval   time.Duration `env:"VAULT_REFRESH_INTERVAL" envDefault:"30m"`
	PETaskAccount          string        `env:"PE_TASK_ACCOUNT"`
	PCTaskAccount          string        `env:"PC_TASK_ACCOUNT"`
}

func NewConfig() (*Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

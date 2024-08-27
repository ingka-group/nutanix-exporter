# _Nutanix Prism Prometheus Exporter_

## About

The Nutanix Exporter is a Go application that fetches live data from any number of Prism Element servers and presents it in a format ingestable by Prometheus. It runs as a docker container, automatically fetching all PE clusters from a Prism Central instance and exporting metrics from multiple APIv2 endpoints (VMs, Hosts, Clusters, etc.).

## Features

- YAML config files define which metrics to collect
- Hashicorp Vault support for fetching cluster credentials
- Refreshes credentials from Vault on 4xx errors
- Parent Exporter class that can be extended for any APIv2 endpoint
- Per cluster metrics exposed at `/metrics/cluster-name`
- Optional filtering by cluster name prefix

## Getting Started

### Prerequisites

- Hashicorp Vault server with KVv2 Secrets Engine enabled
  - Secrets Engine name: defined in `VAULT_ENGINE_NAME` environment variable
  - Secret name: defined in `PE_TASK_ACCOUNT` and `PC_TASK_ACCOUNT` environment variables
  - Fields: username, secret
- Nutanix Prism Central 2023.4 or later

### Metrics Configuration

Metrics are collected from the Prism Element v2.0 APIs. Currently, the exporter supports the following endpoints:

- Clusters
- Hosts
- VMs
- Storage Containers

The response from the API contains a list of entities, each with a set of key-value pairs. The exporter will flatten these key-value pairs and expose them as Prometheus metrics.

`/config` contains a YAML configuration file for each exporter. This is where the metrics to be collected are defined. Any value in the API response can be collected; however, the exporter will only collect metrics that are defined in the configuration file.

Each entry must have the following fields:

- name: The name of the metric key in the API response
- help: User defined description of the metric

```yaml
- name: memory_mb
  help: Memory in MB.
- name: power_state
  help: Power state of the VM.
- name: vcpu_reservation_hz
  help: vCPU reservation in Hz.
- name: stats_num_iops
  help: Number of IOPS. (Example of a nested key where stats is the parent in the response)
```

Default configuration files are provided for each APIv2 endpoint. These can be overwritten when running the exporter by mounting a new configuration file into the container as seen in the deployment section.

## Running the Exporter

While the exporter is designed to run in a containerized environment, it can also be run natively on a host. The following instructions will guide you through both methods. For production environments, the exporter should always be run in a container. However, for development and testing, running the exporter natively is generally easier.

For both methods, start by cloning the repository and navigating to the repo root in your terminal.

To build and run the Go binary natively:

1. Download and install Go from [here](https://go.dev/doc/install)
2. Export all necessary environment variables
3. `go run cmd/nutanix_exporter/main.go`
4. The exporter will now be running on `localhost:9408`

To build and run in a container:

1. Download and install Docker from [here](https://docs.docker.com/get-docker/)
2. `docker build -t nutanix_exporter .`
3. `docker run -p 9408:9408 --env-file configs/exporter.env nutanix_exporter`
4. The exporter will now be running on `localhost:9408`

Example exporter.env:

```yaml
VAULT_ADDR=https://your-vault-server.yourdomain.com
VAULT_NAMESPACE=production
VAULT_ENGINE_NAME=NutanixKV2
VAULT_ROLE_ID=12345678-1234-5678-1234-567812345678
VAULT_SECRET_ID=12345678-1234-5678-1234-567812345678
PC_CLUSTER_NAME=your-pc-cluster-name
PC_CLUSTER_URL=https://your-pc-cluster.yourdomain.com:9440
PE_TASK_ACCOUNT=PETaskAccount
PC_TASK_ACCOUNT=PCTaskAccount
CLUSTER_PREFIX=optional-cluster-prefix to filter cluster names
```

## Deployment

Example docker-compose.yml:

```yaml
services:
  NutanixExporter:
    image: "your_container_registry/nutanix_exporter:latest"
    restart: unless-stopped
    volumes:
      - /path/to/your/configs/cluster.yaml:configs/cluster.yaml:z
      - /path/to/your/configs/storagecontainer.yaml:configs/storagecontainer.yaml:z
      - /path/to/your/configs/host.yaml:configs/host.yaml:z
      - /path/to/your/configs/vm.yaml:configs/vm.yaml:z
    env_file:
      - /path/to/your/configs/exporter.env
    ports:
      - '9408:9408'

```

## Built With

- [Go](https://golang.org/) - Programming language
- [Go Prometheus Client](https://github.com/prometheus/client_golang) - Prometheus client library for Go
- [Go Hashicorp Vault Client](github.com/hashicorp/vault-client-go) - Hashicorp Vault client library for Go
- [Docker](https://www.docker.com/) - Containerization
- [GitHub Actions](https://docs.github.com/en/actions) - CI/CD pipeline

## Community

We do not have any dedicated Slack channel for this project, however the GitHub Discussions feature is enabled for this repository. Feel free to ask questions, share ideas, or discuss the project.

## Contributing

Please read [CONTRIBUTING](./CONTRIBUTING.md) for more details about making a contribution to this open source project and ensure that you follow our [CODE_OF_CONDUCT](./CODE_OF_CONDUCT.md).

## Contact

If you have any other issues or questions regarding this project, feel free to contact one of the [CODEOWNERS](.github/CODEOWNERS) for a more in-depth discussion.

## Licence

This project is open source under the "GPLv2 License", read the [LICENCE](./LICENCE.md) terms for more details.

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
	"errors"
	"os"
	"regexp"
	"strings"
)

// EnvCredentialProvider implements the methods for getting Nutanix
// cluster authentication credentials from environment variables.
type EnvCredentialProvider struct{}

// NewEnvCredentialProvider creates a new instance of EnvCredentialProvider.
func NewEnvCredentialProvider() *EnvCredentialProvider {
	return &EnvCredentialProvider{}
}

// Refresh is a no-op for the EnvCredentialProvider since it does not maintain a state.
func (e *EnvCredentialProvider) Refresh() error {
	return nil
}

// getEnvVarsForCluster generates the names for the environment variables
// that have to be used for providing login credentials of a Nutanix cluster.
func (e *EnvCredentialProvider) getEnvVarsForCluster(cluster string, isPC bool) (string, string) {
	if isPC {
		return "PC_USERNAME", "PC_PASSWORD"
	}

	convertedCluster := e.convertClusterName(cluster)
	return "PE_USERNAME_" + convertedCluster, "PE_PASSWORD_" + convertedCluster
}

// getCreds reads the appropriate environment variables and returns the username and password
// for the provided cluster name.
//
// If isPC is true the credentials for Prism Central will be returned, otherwise those for Prism Element.
func (e *EnvCredentialProvider) getCreds(cluster string, isPC bool) (string, string, error) {
	evU, evP := e.getEnvVarsForCluster(cluster, isPC)

	username := os.Getenv(evU)
	if username == "" {
		return "", "", errors.New("environment variable " + evU + " not set for cluster " + cluster)
	}

	password := os.Getenv(evP)
	if password == "" {
		return "", "", errors.New("environment variable " + evP + " not set for cluster " + cluster)
	}

	return username, password, nil
}

// GetPCCreds retrieves the username and password for a given cluster from environment variables.
func (e *EnvCredentialProvider) GetPCCreds(cluster string) (string, string, error) {
	return e.getCreds(cluster, true)
}

// GetPECreds retrieves the username and password for a given cluster from environment variables.
func (e *EnvCredentialProvider) GetPECreds(cluster string) (string, string, error) {
	return e.getCreds(cluster, false)
}

// convertClusterName converts the cluster name to uppercase and replaces non-alphanumeric characters with underscores.
func (e *EnvCredentialProvider) convertClusterName(cluster string) string {
	cluster = strings.ToUpper(cluster)
	r, _ := regexp.Compile("[^A-Z_0-9]+")
	return r.ReplaceAllString(cluster, "_")
}

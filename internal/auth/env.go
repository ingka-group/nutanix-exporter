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

// ConvertClusterName changes any characters that should not be used in
// environment variables to an underscore (_).
func ConvertClusterName(cluster string) string {
	cluster = strings.ToUpper(cluster)

	r, _ := regexp.Compile("[^A-Z_0-9]+")
	cluster = r.ReplaceAllString(cluster, "_")

	return cluster
}

// getEnvVarsForCluster generates the names for the environment variables
// that have to be used for providing login credentials of a Nutanix cluster.
func getEnvVarsForCluster(cluster string, isPC bool) (string, string) {
	var evU, evP string

	if isPC {
		evU = "PC_USERNAME"
		evP = "PC_PASSWORD"
	} else {
		evU = "PE_USERNAME_" + ConvertClusterName(cluster)
		evP = "PE_PASSWORD_" + ConvertClusterName(cluster)
	}

	return evU, evP
}

// getCreds reads the appropriate environment variables and returns the username and password
// for the provided cluster name.
//
// If isPC is true the credentials for Prism Central will be returned, otherwise those for Prism Element.
func getCreds(cluster string, isPC bool) (string, string, error) {
	evU, evP := getEnvVarsForCluster(cluster, isPC)

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

// GetPCCreds returns the username and password for the specified Prism Central cluster
func (evCP *EnvCredentialProvider) GetPCCreds(cluster string) (string, string, error) {

	return getCreds(cluster, true)
}

// GetPECreds returns the username and password for the specified Prism Element cluster
func (evCP *EnvCredentialProvider) GetPECreds(cluster string) (string, string, error) {

	return getCreds(cluster, false)
}

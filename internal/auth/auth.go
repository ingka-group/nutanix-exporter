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

// Package auth handles retrieving the correct credentials for authenticating with
// Prism Central and Prism Element clusters.
//
// Supported credential providers are:
// - HashiCorp Vault
// - Environment variables

package auth

import (
	"log"
	"os"
)

// CredentialProvider defines which functions a credential provider for Nutanix
// cluster authentication has to implement.
type CredentialProvider interface {
	GetPCCreds(cluster string) (string, string, error)
	GetPECreds(cluster string) (string, string, error)
}

// getEnvOrFatal returns the value of the specified environment variable or exits the program.
func getEnvOrFatal(envVar string) string {
	value := os.Getenv(envVar)
	if value == "" {
		log.Fatalf("%s environment variable is not set", envVar)
	}
	return value
}

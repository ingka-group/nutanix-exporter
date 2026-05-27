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

package auth

import (
	"strings"
	"testing"
)

func Test_convertClusterName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"lowercase", "mycluster", "MYCLUSTER"},
		{"already uppercase", "MYCLUSTER", "MYCLUSTER"},
		{"hyphen", "my-cluster", "MY_CLUSTER"},
		{"dot", "my.cluster", "MY_CLUSTER"},
		{"space", "my cluster", "MY_CLUSTER"},
		{"mixed separators", "my-cluster.name here", "MY_CLUSTER_NAME_HERE"},
		{"consecutive separators", "my--cluster", "MY_CLUSTER"},
		{"leading separator", "-cluster", "_CLUSTER"},
		{"trailing separator", "cluster-", "CLUSTER_"},
		{"alphanumeric preserved", "cluster01", "CLUSTER01"},
		{"empty string", "", ""},
	}

	e := NewEnvCredentialProvider()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.convertClusterName(tt.input)
			if got != tt.want {
				t.Errorf("convertClusterName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func Test_getEnvVarsForCluster(t *testing.T) {
	tests := []struct {
		name        string
		cluster     string
		isPC        bool
		wantUserVar string
		wantPassVar string
	}{
		{
			name:        "prism central ignores cluster name",
			cluster:     "any-cluster",
			isPC:        true,
			wantUserVar: "PC_USERNAME",
			wantPassVar: "PC_PASSWORD",
		},
		{
			name:        "prism element simple name",
			cluster:     "mycluster",
			isPC:        false,
			wantUserVar: "PE_USERNAME_MYCLUSTER",
			wantPassVar: "PE_PASSWORD_MYCLUSTER",
		},
		{
			name:        "prism element hyphenated name",
			cluster:     "my-cluster",
			isPC:        false,
			wantUserVar: "PE_USERNAME_MY_CLUSTER",
			wantPassVar: "PE_PASSWORD_MY_CLUSTER",
		},
		{
			name:        "prism element alphanumeric",
			cluster:     "cluster01",
			isPC:        false,
			wantUserVar: "PE_USERNAME_CLUSTER01",
			wantPassVar: "PE_PASSWORD_CLUSTER01",
		},
	}

	e := NewEnvCredentialProvider()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotU, gotP := e.getEnvVarsForCluster(tt.cluster, tt.isPC)
			if gotU != tt.wantUserVar {
				t.Errorf("getEnvVarsForCluster(%q, %v) username var = %q, want %q", tt.cluster, tt.isPC, gotU, tt.wantUserVar)
			}
			if gotP != tt.wantPassVar {
				t.Errorf("getEnvVarsForCluster(%q, %v) password var = %q, want %q", tt.cluster, tt.isPC, gotP, tt.wantPassVar)
			}
		})
	}
}

func Test_getCreds(t *testing.T) {
	e := NewEnvCredentialProvider()

	t.Run("PC creds present", func(t *testing.T) {
		t.Setenv("PC_USERNAME", "admin")
		t.Setenv("PC_PASSWORD", "secret")

		u, p, err := e.getCreds("any-cluster", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if u != "admin" || p != "secret" {
			t.Errorf("getCreds() = (%q, %q), want (\"admin\", \"secret\")", u, p)
		}
	})

	t.Run("PE creds present", func(t *testing.T) {
		t.Setenv("PE_USERNAME_MY_CLUSTER", "peuser")
		t.Setenv("PE_PASSWORD_MY_CLUSTER", "pepass")

		u, p, err := e.getCreds("my-cluster", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if u != "peuser" || p != "pepass" {
			t.Errorf("getCreds() = (%q, %q), want (\"peuser\", \"pepass\")", u, p)
		}
	})

	t.Run("username env var missing", func(t *testing.T) {
		t.Setenv("PC_USERNAME", "")
		t.Setenv("PC_PASSWORD", "secret")

		_, _, err := e.getCreds("any-cluster", true)
		if err == nil {
			t.Fatal("expected error when username env var missing, got nil")
		}
	})

	t.Run("password env var missing", func(t *testing.T) {
		t.Setenv("PC_USERNAME", "admin")
		t.Setenv("PC_PASSWORD", "")

		_, _, err := e.getCreds("any-cluster", true)
		if err == nil {
			t.Fatal("expected error when password env var missing, got nil")
		}
	})

	t.Run("error message contains var name", func(t *testing.T) {
		t.Setenv("PE_USERNAME_PROD", "")

		_, _, err := e.getCreds("prod", false)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "PE_USERNAME_PROD") {
			t.Errorf("error message %q should contain env var name %q", err.Error(), "PE_USERNAME_PROD")
		}
	})
}

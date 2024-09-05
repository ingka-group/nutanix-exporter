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

package nutanix

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ingka-group/nutanix-exporter/internal/auth"

	"github.com/prometheus/client_golang/prometheus"
)

type NutanixClient interface {
	RefreshCredentials(vaultClient *auth.VaultClient) error
	CreateRequest(ctx context.Context, reqType, action string, p RequestParams) (*http.Request, error)
	MakeRequestWithParams(ctx context.Context, reqType, action string, p RequestParams) (*http.Response, error)
	MakeRequest(ctx context.Context, reqType, action string) (*http.Response, error)
}

// Cluster represents a Nutanix cluster (Prism Central OR Element)
type Cluster struct {
	Name          string
	URL           string `yaml:"URL"`
	API           NutanixClient
	Registry      *prometheus.Registry
	Collectors    []prometheus.Collector
	RefreshNeeded bool
	Mutex         sync.Mutex
}

// PEClient represents the Prism Element API client
type PEClient struct {
	URL           string
	Username      string
	Password      string
	SkipTLSVerify bool
	Timeout       time.Duration
}

// PCClient represents the Prism Central API client
type PCClient struct {
	URL           string
	Username      string
	Password      string
	SkipTLSVerify bool
	Timeout       time.Duration
}

// RequestParams holds the components for a request (body, header, params)
type RequestParams struct {
	Body   string
	Header string
	Params url.Values
}

// NewCluster returns a new Nutanix cluster object, fetching credentials and creating an API client.
func NewCluster(name, url string, vaultClient *auth.VaultClient, isPC bool, skipTLSVerify bool, timeout time.Duration) *Cluster {
	var api NutanixClient
	var username, password string

	if isPC {
		username, password = vaultClient.GetPCCreds(name)
		if username == "" || password == "" {
			log.Printf("Failed to get credentials for Prism Central %s", name)
			return nil
		}
		api = NewPCClient(url, username, password, skipTLSVerify, timeout)
	} else {
		username, password = vaultClient.GetPECreds(name)
		if username == "" || password == "" {
			log.Printf("Failed to get credentials for Prism Element %s", name)
			return nil
		}
		api = NewPEClient(url, username, password, skipTLSVerify, timeout)
	}

	return &Cluster{
		Name:     name,
		URL:      url,
		API:      api,
		Registry: prometheus.NewRegistry(),
	}
}

// NewPEClient returns a new Prism Element client object
func NewPEClient(url, username, password string, skipTLSVerify bool, timeout time.Duration) *PEClient {
	return &PEClient{
		URL:           url,
		Username:      username,
		Password:      password,
		SkipTLSVerify: skipTLSVerify,
		Timeout:       timeout,
	}
}

// NewPCClient returns a new Prism Central client object
func NewPCClient(url, username, password string, skipTLSVerify bool, timeout time.Duration) *PCClient {
	return &PCClient{
		URL:           url,
		Username:      username,
		Password:      password,
		SkipTLSVerify: skipTLSVerify,
		Timeout:       timeout,
	}
}

// Refreshes stale credentials using client methods
func (c *Cluster) RefreshCredentialsIfNeeded(vaultClient *auth.VaultClient) {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	if c.RefreshNeeded {
		if err := c.API.RefreshCredentials(vaultClient); err != nil {
			log.Printf("Failed to refresh credentials for cluster %s: %v", c.Name, err)
			return
		}
		c.RefreshNeeded = false // Reset the flag after refreshing
		log.Printf("Credentials refreshed for cluster %s", c.Name)
	}
}

// RefreshCredentials refreshes the credentials for the PEClient
func (c *PEClient) RefreshCredentials(vaultClient *auth.VaultClient) error {
	username, password := vaultClient.GetPECreds(c.URL)
	if username == "" || password == "" {
		return fmt.Errorf("failed to refresh credentials for PE client %s", c.URL)
	}
	c.Username = username
	c.Password = password
	return nil
}

// RefreshCredentials refreshes the credentials for the PCClient
func (c *PCClient) RefreshCredentials(vaultClient *auth.VaultClient) error {
	username, password := vaultClient.GetPCCreds(c.URL)
	if username == "" || password == "" {
		return fmt.Errorf("failed to refresh credentials for PC client %s", c.URL)
	}
	c.Username = username
	c.Password = password
	return nil
}

// CreateRequest takes context, request type, action and request parameters
// Returns a new http request
// Helper for making requests to Prism Element
func (c *PEClient) CreateRequest(ctx context.Context, reqType, action string, p RequestParams) (*http.Request, error) {
	fullURL := fmt.Sprintf("%s/PrismGateway/services/rest/%s/", strings.Trim(c.URL, "/"), strings.Trim(action, "/"))

	log.Printf("Sending request to %s", fullURL)

	req, err := http.NewRequestWithContext(ctx, reqType, fullURL, strings.NewReader(p.Body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.Username, c.Password)

	return req, nil
}

// CreateRequest takes context, request type, action and request parameters
// Returns a new http request
// Helper for making requests to Prism Central
func (c *PCClient) CreateRequest(ctx context.Context, reqType, action string, p RequestParams) (*http.Request, error) {
	fullURL := fmt.Sprintf("%s/%s", strings.Trim(c.URL, "/"), strings.Trim(action, "/"))

	log.Printf("Sending request to %s", fullURL)

	req, err := http.NewRequestWithContext(ctx, reqType, fullURL, strings.NewReader(p.Body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.Username, c.Password)

	return req, nil
}

// MakeRequestWithParams takes context, request type, action and request parameters
// Returns a new http response
func (c *PEClient) MakeRequestWithParams(ctx context.Context, reqType, action string, p RequestParams) (*http.Response, error) {
	req, err := c.CreateRequest(ctx, reqType, action, p)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: c.SkipTLSVerify},
		},
		Timeout: c.Timeout,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

// MakeRequestWithParams takes context, request type, action and request parameters
// Returns a new http response for PCClient
func (c *PCClient) MakeRequestWithParams(ctx context.Context, reqType, action string, p RequestParams) (*http.Response, error) {
	req, err := c.CreateRequest(ctx, reqType, action, p)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: c.SkipTLSVerify},
		},
		Timeout: c.Timeout,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}

	return resp, nil
}

// MakeRequest takes context, request type and action
// Returns a new http response
// Calls MakeRequestWithParams with empty RequestParams for PEClient
func (c *PEClient) MakeRequest(ctx context.Context, reqType, action string) (*http.Response, error) {
	return c.MakeRequestWithParams(ctx, reqType, action, RequestParams{})
}

// MakeRequest takes context, request type and action
// Returns a new http response
// Calls MakeRequestWithParams with empty RequestParams for PCClient
func (c *PCClient) MakeRequest(ctx context.Context, reqType, action string) (*http.Response, error) {
	return c.MakeRequestWithParams(ctx, reqType, action, RequestParams{})
}

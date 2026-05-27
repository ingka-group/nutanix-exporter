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

package nutanix

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ingka-group/nutanix-exporter/internal/auth"
)

// NutanixClient is the interface implemented by all API clients.
type NutanixClient interface {
	MakeRequest(ctx context.Context, method, action string, opts ...RequestOptions) (*http.Response, error)
}

// Client is a single HTTP client for either Prism Element or Prism Central.
// The basePath field captures the URL-prefix difference between the two:
//   - PE: "/PrismGateway/services/rest"
//   - PC: "" (bare path)
type Client struct {
	baseURL      string
	basePath     string
	clusterName  string // used as the key for credential lookups
	username     string
	password     string
	credProvider auth.CredentialProvider
	isPC         bool
	httpClient   *http.Client
	credsMu      sync.RWMutex
	refreshMu    sync.Mutex // serialises refreshCreds; only one goroutine refreshes at a time
	lastRefresh  time.Time  // others that arrive after a recent refresh skip calling it again
}

// RequestOptions holds optional components for a request.
type RequestOptions struct {
	Params  url.Values
	Payload any
	Body    string
}

// newClient constructs a Client. basePath is set based on whether this targets PE or PC.
func newClient(clusterName, baseURL, username, password string, ncp auth.CredentialProvider, isPC bool, skipTLSVerify bool, timeout time.Duration) *Client {
	basePath := ""
	if !isPC {
		basePath = "/PrismGateway/services/rest"
	}

	return &Client{
		baseURL:      strings.TrimRight(baseURL, "/"),
		basePath:     basePath,
		clusterName:  clusterName,
		username:     username,
		password:     password,
		credProvider: ncp,
		isPC:         isPC,
		httpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: skipTLSVerify}, //nolint:gosec
			},
			Timeout: timeout,
		},
	}
}

// NewPEClient returns a NutanixClient targeting a Prism Element cluster.
func NewPEClient(clusterName, rawURL, username, password string, ncp auth.CredentialProvider, skipTLSVerify bool, timeout time.Duration) NutanixClient {
	return newClient(clusterName, rawURL, username, password, ncp, false, skipTLSVerify, timeout)
}

// NewPCClient returns a NutanixClient targeting Prism Central.
func NewPCClient(clusterName, rawURL, username, password string, ncp auth.CredentialProvider, skipTLSVerify bool, timeout time.Duration) NutanixClient {
	return newClient(clusterName, rawURL, username, password, ncp, true, skipTLSVerify, timeout)
}

// buildRequest constructs an HTTP request from method, action path, and optional opts.
func (c *Client) buildRequest(ctx context.Context, method, action string, opt RequestOptions) (*http.Request, error) {
	rawURL := fmt.Sprintf("%s%s/%s", c.baseURL, c.basePath, strings.Trim(action, "/"))

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	if opt.Params != nil {
		q := parsedURL.Query()
		for key, values := range opt.Params {
			for _, v := range values {
				q.Add(key, v)
			}
		}
		parsedURL.RawQuery = q.Encode()
	}

	fullURL := parsedURL.String()
	slog.Info("Sending request", "url", fullURL, "method", method)

	var req *http.Request
	if opt.Payload != nil {
		jsonPayload, err := json.Marshal(opt.Payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal payload: %w", err)
		}
		req, err = http.NewRequestWithContext(ctx, method, fullURL, strings.NewReader(string(jsonPayload)))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequestWithContext(ctx, method, fullURL, strings.NewReader(opt.Body))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
	}

	c.credsMu.RLock()
	req.SetBasicAuth(c.username, c.password)
	c.credsMu.RUnlock()

	return req, nil
}

// MakeRequest executes a request with optional RequestOptions. On a 401/403 it refreshes
// all credentials and retries once.
func (c *Client) MakeRequest(ctx context.Context, method, action string, opts ...RequestOptions) (*http.Response, error) {
	var opt RequestOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	req, err := c.buildRequest(ctx, method, action, opt)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		_ = resp.Body.Close()

		slog.Warn("Authentication failed, refreshing credentials and retrying", "url", req.URL.String())
		if refreshErr := c.refreshCreds(); refreshErr != nil {
			return nil, fmt.Errorf("credential refresh failed: %w", refreshErr)
		}

		retryReq, err := c.buildRequest(ctx, method, action, opt)
		if err != nil {
			return nil, fmt.Errorf("failed to build retry request: %w", err)
		}

		resp, err = c.httpClient.Do(retryReq)
		if err != nil {
			return nil, fmt.Errorf("retry request failed: %w", err)
		}
	}

	return resp, nil
}

// refreshCreds refreshes credentials at most once per second. Concurrent callers block on
// refreshMu; if a refresh just completed by the time they acquire the lock they return
// immediately, picking up the credentials the first caller already fetched.
func (c *Client) refreshCreds() error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	// If another goroutine already refreshed within the last second, skip.
	if time.Since(c.lastRefresh) < time.Second {
		return nil
	}

	if err := c.credProvider.Refresh(); err != nil {
		return fmt.Errorf("provider refresh failed: %w", err)
	}

	var username, password string
	var err error

	if c.isPC {
		username, password, err = c.credProvider.GetPCCreds(c.clusterName)
	} else {
		username, password, err = c.credProvider.GetPECreds(c.clusterName)
	}

	if username == "" || password == "" {
		return fmt.Errorf("empty credentials after refresh: %w", err)
	}

	c.credsMu.Lock()
	c.username = username
	c.password = password
	c.credsMu.Unlock()

	c.lastRefresh = time.Now()
	return nil
}

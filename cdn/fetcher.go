/*
Copyright © 2026 Benny Powers <web@bennypowers.com>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program. If not, see <http://www.gnu.org/licenses/>.
*/

// Package cdn provides HTTP fetching abstractions for CDN-based package resolution.
package cdn

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// Fetcher provides an abstraction over HTTP fetching.
type Fetcher interface {
	// Fetch retrieves content from the given URL.
	// Returns the response body bytes or an error if the request fails.
	Fetch(ctx context.Context, url string) ([]byte, error)
}

// HTTPFetcher implements Fetcher using net/http.
// Works in both native and WASM builds (WASM uses browser Fetch API automatically).
type HTTPFetcher struct {
	client *http.Client
}

// NewHTTPFetcher creates a new HTTP fetcher.
func NewHTTPFetcher() *HTTPFetcher {
	return &HTTPFetcher{
		client: &http.Client{},
	}
}

// NewHTTPFetcherWithClient creates a new HTTP fetcher with a custom client.
func NewHTTPFetcherWithClient(client *http.Client) *HTTPFetcher {
	return &HTTPFetcher{
		client: client,
	}
}

// Fetch retrieves content from the given URL.
func (f *HTTPFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &FetchError{URL: url, Message: err.Error()}
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, &FetchError{URL: url, Message: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, &FetchError{
			URL:        url,
			StatusCode: resp.StatusCode,
			Message:    resp.Status,
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &FetchError{URL: url, Message: err.Error()}
	}

	return body, nil
}

// FetchError represents an HTTP fetch error with status information.
type FetchError struct {
	URL        string
	StatusCode int
	Message    string
}

func (e *FetchError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("fetch %s: HTTP %d: %s", e.URL, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("fetch %s: %s", e.URL, e.Message)
}

// IsNotFound returns true if the error represents a 404 Not Found response.
func (e *FetchError) IsNotFound() bool {
	return e.StatusCode == 404
}

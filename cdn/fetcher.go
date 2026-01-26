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

	"github.com/tinywasm/fetch"
)

// Fetcher provides an abstraction over HTTP fetching.
type Fetcher interface {
	// Fetch retrieves content from the given URL.
	// Returns the response body bytes or an error if the request fails.
	Fetch(ctx context.Context, url string) ([]byte, error)
}

// HTTPFetcher implements Fetcher using tinywasm/fetch.
// Works in both native and WASM builds with minimal binary size.
type HTTPFetcher struct{}

// NewHTTPFetcher creates a new HTTP fetcher.
func NewHTTPFetcher() *HTTPFetcher {
	return &HTTPFetcher{}
}

// Fetch retrieves content from the given URL.
func (f *HTTPFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	type result struct {
		body []byte
		err  error
	}
	done := make(chan result, 1)

	fetch.Get(url).Send(func(resp *fetch.Response, err error) {
		if err != nil {
			done <- result{nil, &FetchError{URL: url, Message: err.Error()}}
			return
		}
		if resp.Status != 200 {
			done <- result{nil, &FetchError{
				URL:        url,
				StatusCode: resp.Status,
				Message:    fmt.Sprintf("HTTP %d", resp.Status),
			}}
			return
		}
		done <- result{resp.Body(), nil}
	})

	select {
	case r := <-done:
		return r.body, r.err
	case <-ctx.Done():
		return nil, &FetchError{URL: url, Message: ctx.Err().Error()}
	}
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

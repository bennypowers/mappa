//go:build !js

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

package cdn

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// fetch retrieves url with net/http, enforcing the size limit while reading.
// Context cancellation and deadlines abort the underlying request.
func (f *HTTPFetcher) fetch(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	maxSize := f.effectiveMaxResponseSize()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, newFetchError(url, err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, newContextFetchError(url, ctxErr)
		}
		return nil, newFetchError(url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, &FetchError{
			URL:        url,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("HTTP %d", resp.StatusCode),
		}
	}
	if resp.ContentLength > int64(maxSize) {
		return nil, newTooLargeError(url, maxSize)
	}

	// Read one byte past the limit so an oversized stream is detected without
	// buffering the whole body.
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxSize)+1))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, newContextFetchError(url, ctxErr)
		}
		return nil, newFetchError(url, err)
	}
	if len(body) > maxSize {
		return nil, newTooLargeError(url, maxSize)
	}
	return body, nil
}

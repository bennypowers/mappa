//go:build js

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
	"time"

	"github.com/tinywasm/fetch"
)

// fetch retrieves url with tinywasm/fetch under WASM. The library delivers a
// fully buffered body, so the size limit is validated after transfer rather
// than enforced while reading; native builds stream through net/http instead.
func (f *HTTPFetcher) fetch(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	type result struct {
		body []byte
		err  error
	}
	done := make(chan result, 1)

	req := fetch.Get(url)
	for key, value := range headers {
		req = req.Header(key, value)
	}
	deadline, hasDeadline := ctx.Deadline()
	if hasDeadline {
		if ms := int(time.Until(deadline).Milliseconds()); ms > 0 {
			req = req.Timeout(ms)
		}
	}

	maxSize := f.effectiveMaxResponseSize()

	req.Send(func(resp *fetch.Response, err error) {
		if err != nil {
			// The transport may report an empty or unhelpful error when the
			// request timeout fires. Fall back to the context state, which
			// shares the same deadline.
			ctxErr := ctx.Err()
			if ctxErr == nil && hasDeadline && time.Until(deadline) <= 10*time.Millisecond {
				// The transport timeout shares the context deadline but may
				// fire a hair earlier; treat an error inside that window as a
				// timeout.
				ctxErr = context.DeadlineExceeded
			}
			if ctxErr != nil {
				done <- result{nil, newContextFetchError(url, ctxErr)}
				return
			}
			done <- result{nil, newFetchError(url, err)}
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
		body := resp.Body()
		if len(body) > maxSize {
			done <- result{nil, newTooLargeError(url, maxSize)}
			return
		}
		done <- result{body, nil}
	})

	select {
	case r := <-done:
		return r.body, r.err
	case <-ctx.Done():
		return nil, newContextFetchError(url, ctx.Err())
	}
}

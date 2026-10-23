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
	"errors"
	"fmt"
	"strings"
)

// DefaultMaxResponseSize caps the size of a fetched response body (64 MiB).
// Real registry packuments (for example vite) exceed 32 MiB, so the default
// must be large enough to admit them.
//
// The check runs after the fetch implementation has buffered the body, so this
// is a validation limit, not a transfer or memory bound.
const DefaultMaxResponseSize = 64 << 20

// Sentinel errors exposed by FetchError so callers can classify failures
// without matching on message text.
var (
	// ErrTimeout indicates that the request timed out or was aborted.
	ErrTimeout = errors.New("fetch timed out")
	// ErrResponseTooLarge indicates that the response exceeded the configured
	// size limit.
	ErrResponseTooLarge = errors.New("response exceeds maximum size")
)

// Fetcher provides an abstraction over HTTP fetching.
type Fetcher interface {
	// Fetch retrieves content from the given URL.
	// Returns the response body bytes or an error if the request fails.
	Fetch(ctx context.Context, url string) ([]byte, error)
}

// HeaderFetcher is an optional Fetcher extension that supports per-request
// headers. Callers can type-assert a Fetcher to this interface to send media
// type hints such as the abbreviated npm packument Accept header.
type HeaderFetcher interface {
	FetchWithHeaders(ctx context.Context, url string, headers map[string]string) ([]byte, error)
}

// HTTPFetcher implements Fetcher.
// Native builds stream through net/http and enforce the size limit while
// reading; WASM builds use tinywasm/fetch, which buffers before delivery.
type HTTPFetcher struct {
	maxResponseSize int
}

// NewHTTPFetcher creates a new HTTP fetcher with DefaultMaxResponseSize.
func NewHTTPFetcher() *HTTPFetcher {
	return &HTTPFetcher{maxResponseSize: DefaultMaxResponseSize}
}

// WithMaxResponseSize returns a copy of the fetcher with the given response
// size limit in bytes. Values <= 0 restore DefaultMaxResponseSize.
func (f *HTTPFetcher) WithMaxResponseSize(n int) *HTTPFetcher {
	if n <= 0 {
		n = DefaultMaxResponseSize
	}
	c := *f
	c.maxResponseSize = n
	return &c
}

// Fetch retrieves content from the given URL.
// Note: cancellation without a deadline stops waiting for the response but does
// not abort the underlying HTTP request due to the async callback pattern.
// A context deadline, however, is forwarded to the request as its timeout and
// does abort it.
func (f *HTTPFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	return f.fetch(ctx, url, nil)
}

// FetchWithHeaders retrieves content from the given URL with additional
// request headers.
func (f *HTTPFetcher) FetchWithHeaders(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	return f.fetch(ctx, url, headers)
}

// effectiveMaxResponseSize returns the configured limit or the default.
func (f *HTTPFetcher) effectiveMaxResponseSize() int {
	if f.maxResponseSize <= 0 {
		return DefaultMaxResponseSize
	}
	return f.maxResponseSize
}

// newTooLargeError builds the error for a response over the configured limit.
func newTooLargeError(url string, maxSize int) *FetchError {
	return &FetchError{
		URL:     url,
		Message: fmt.Sprintf("response exceeds maximum size (%d bytes)", maxSize),
		Err:     ErrResponseTooLarge,
	}
}

// newFetchError classifies an error returned by the transport.
func newFetchError(url string, err error) *FetchError {
	fe := &FetchError{URL: url, Message: err.Error()}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "deadline exceeded"):
		fe.Err = fmt.Errorf("%w: %w", ErrTimeout, context.DeadlineExceeded)
	case strings.Contains(lower, "timeout"), strings.Contains(lower, "abort"):
		fe.Err = ErrTimeout
	}
	return fe
}

// newContextFetchError classifies a context cancellation.
func newContextFetchError(url string, ctxErr error) *FetchError {
	fe := &FetchError{URL: url, Message: ctxErr.Error(), Err: ctxErr}
	if errors.Is(ctxErr, context.DeadlineExceeded) {
		fe.Err = fmt.Errorf("%w: %w", ErrTimeout, context.DeadlineExceeded)
	}
	return fe
}

// FetchError represents an HTTP fetch error with status information.
type FetchError struct {
	URL        string
	StatusCode int
	Message    string
	// Err is the underlying cause, if any. It identifies timeouts, oversized
	// responses, and context cancellation.
	Err error
}

func (e *FetchError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("fetch %s: HTTP %d: %s", e.URL, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("fetch %s: %s", e.URL, e.Message)
}

// Unwrap returns the underlying cause, enabling errors.Is and errors.As.
func (e *FetchError) Unwrap() error {
	return e.Err
}

// IsNotFound returns true if the error represents a 404 Not Found response.
func (e *FetchError) IsNotFound() bool {
	return e.StatusCode == 404
}

// IsTimeout reports whether the error came from a request timeout.
func (e *FetchError) IsTimeout() bool {
	return IsTimeout(e)
}

// IsTooLarge reports whether the error came from an oversized response.
func (e *FetchError) IsTooLarge() bool {
	return IsTooLarge(e)
}

// IsNotFound reports whether err (or an error it wraps) is a 404 response.
func IsNotFound(err error) bool {
	var fe *FetchError
	return errors.As(err, &fe) && fe.StatusCode == 404
}

// IsTimeout reports whether err (or an error it wraps) is a timeout.
func IsTimeout(err error) bool {
	return errors.Is(err, ErrTimeout) || errors.Is(err, context.DeadlineExceeded)
}

// IsTooLarge reports whether err (or an error it wraps) exceeded the response
// size limit.
func IsTooLarge(err error) bool {
	return errors.Is(err, ErrResponseTooLarge)
}

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
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPFetcherCapsResponseSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("x", 2048)))
	}))
	defer server.Close()

	_, err := NewHTTPFetcher().WithMaxResponseSize(1024).Fetch(context.Background(), server.URL)
	if err == nil {
		t.Fatal("expected oversized response to be rejected")
	}
	if !strings.Contains(err.Error(), "maximum size") {
		t.Errorf("unexpected error: %v", err)
	}
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Errorf("expected ErrResponseTooLarge, got %v", err)
	}
	if !IsTooLarge(err) {
		t.Errorf("expected IsTooLarge to report true for %v", err)
	}
}

func TestHTTPFetcherAcceptsBodyAboveLegacyLimit(t *testing.T) {
	// Real packuments (for example vite) exceed the old 32 MiB limit. The
	// default limit must admit them.
	const legacyLimit = 32 << 20
	body := bytes.Repeat([]byte("x"), legacyLimit+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	got, err := NewHTTPFetcher().Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("expected body above the legacy limit to be accepted: %v", err)
	}
	if len(got) != len(body) {
		t.Errorf("got %d bytes, want %d", len(got), len(body))
	}
	if DefaultMaxResponseSize < 64<<20 {
		t.Errorf("DefaultMaxResponseSize = %d, want at least 64 MiB", DefaultMaxResponseSize)
	}
}

func TestHTTPFetcherBoundsReadWhileStreaming(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("x", 2048)))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	_, err := NewHTTPFetcher().WithMaxResponseSize(1024).Fetch(ctx, server.URL)
	elapsed := time.Since(start)

	if !IsTooLarge(err) {
		t.Fatalf("expected ErrResponseTooLarge, got %v", err)
	}
	if elapsed > time.Second {
		t.Errorf("oversized stream took %v; the limit was likely applied after buffering", elapsed)
	}
}

func TestHTTPFetcherRespectsContextDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewHTTPFetcher().Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("expected cancelled context to fail the fetch")
	}
}

func TestHTTPFetcherAbortsOnDeadline(t *testing.T) {
	// The server sleeps well past the deadline. The deadline must be forwarded
	// to the request timeout so the fetch aborts rather than waiting it out.
	const serverDelay = 500 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(serverDelay):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := NewHTTPFetcher().Fetch(ctx, server.URL)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected deadline to fail the fetch")
	}
	if elapsed >= serverDelay/2 {
		t.Errorf("fetch took %v; expected it to abort near the 30ms deadline", elapsed)
	}
	if !IsTimeout(err) {
		t.Errorf("expected a timeout classification, got %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded in the chain, got %v", err)
	}
}

func TestHTTPFetcherClassifiesTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := NewHTTPFetcher().Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("expected ErrTimeout in chain, got %v", err)
	}
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *FetchError, got %T", err)
	}
	if !fe.IsTimeout() {
		t.Errorf("expected (*FetchError).IsTimeout, got %v", err)
	}
}

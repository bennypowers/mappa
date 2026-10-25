//go:build js && wasm

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
	"os"
	"strings"
	"testing"
	"time"
)

// wasmTestServer returns the base URL of the Node fixture server started by
// scripts/test-wasm.sh.
func wasmTestServer(t *testing.T) string {
	t.Helper()
	base := os.Getenv("MAPPA_WASM_TEST_SERVER_URL")
	if base == "" {
		t.Skip("MAPPA_WASM_TEST_SERVER_URL not set; run make test-wasm")
	}
	return strings.TrimSuffix(base, "/")
}

func TestHTTPFetcherWASMSuccess(t *testing.T) {
	body, err := NewHTTPFetcher().Fetch(context.Background(), wasmTestServer(t)+"/ok")
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if got := string(body); got != `{"ok":true}` {
		t.Errorf("body = %q, want %q", got, `{"ok":true}`)
	}
}

func TestHTTPFetcherWASMNotFound(t *testing.T) {
	_, err := NewHTTPFetcher().Fetch(context.Background(), wasmTestServer(t)+"/status/404")
	if err == nil {
		t.Fatal("expected a 404 to fail the fetch")
	}
	if !IsNotFound(err) {
		t.Errorf("expected IsNotFound, got %v", err)
	}
}

func TestHTTPFetcherWASMOversized(t *testing.T) {
	_, err := NewHTTPFetcher().
		WithMaxResponseSize(1024).
		Fetch(context.Background(), wasmTestServer(t)+"/large")
	if err == nil {
		t.Fatal("expected the oversized response to be rejected")
	}
	if !IsTooLarge(err) {
		t.Errorf("expected ErrResponseTooLarge, got %v", err)
	}
}

func TestHTTPFetcherWASMTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := NewHTTPFetcher().Fetch(ctx, wasmTestServer(t)+"/slow")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected the deadline to fail the fetch")
	}
	if !IsTimeout(err) {
		t.Errorf("expected a timeout classification, got %v", err)
	}
	if elapsed >= 400*time.Millisecond {
		t.Errorf("fetch took %v; expected it to abort near the 50ms deadline", elapsed)
	}
}

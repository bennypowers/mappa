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
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"bennypowers.dev/mappa/testutil"
)

// MockFetcher is a test implementation of the Fetcher interface.
type MockFetcher struct {
	responses map[string][]byte
	errors    map[string]error
}

func NewMockFetcher() *MockFetcher {
	return &MockFetcher{
		responses: make(map[string][]byte),
		errors:    make(map[string]error),
	}
}

func (m *MockFetcher) AddResponse(url string, data []byte) {
	m.responses[url] = data
}

func (m *MockFetcher) AddError(url string, err error) {
	m.errors[url] = err
}

func (m *MockFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	if err, ok := m.errors[url]; ok {
		return nil, err
	}
	if data, ok := m.responses[url]; ok {
		return data, nil
	}
	return nil, &FetchError{URL: url, StatusCode: 404, Message: "Not Found"}
}

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input   string
		want    *SemVer
		wantErr bool
	}{
		{"1.0.0", &SemVer{Major: 1, Minor: 0, Patch: 0}, false},
		{"2.3.4", &SemVer{Major: 2, Minor: 3, Patch: 4}, false},
		{"1.0.0-alpha", &SemVer{Major: 1, Minor: 0, Patch: 0, Prerelease: "alpha"}, false},
		{"1.0.0-beta.1", &SemVer{Major: 1, Minor: 0, Patch: 0, Prerelease: "beta.1"}, false},
		{"v1.0.0", &SemVer{Major: 1, Minor: 0, Patch: 0}, false},
		{"1.0", &SemVer{Major: 1, Minor: 0, Patch: 0}, false},
		{"1", &SemVer{Major: 1, Minor: 0, Patch: 0}, false},
		{"invalid", nil, true},
		{"", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseSemver(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSemver(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if tt.want != nil {
				if got.Major != tt.want.Major || got.Minor != tt.want.Minor ||
					got.Patch != tt.want.Patch || got.Prerelease != tt.want.Prerelease {
					t.Errorf("parseSemver(%q) = %+v, want %+v", tt.input, got, tt.want)
				}
			}
		})
	}
}

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.0.0", "1.1.0", -1},
		{"1.1.0", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0-alpha", "1.0.0", -1},
		{"1.0.0", "1.0.0-alpha", 1},
		{"1.0.0-alpha", "1.0.0-beta", -1},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s vs %s", tt.a, tt.b), func(t *testing.T) {
			got := compareSemver(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("compareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestMatchVersion(t *testing.T) {
	versions := []string{"1.0.0", "1.0.1", "1.1.0", "1.2.0", "2.0.0", "2.1.0", "3.0.0-alpha"}

	tests := []struct {
		name         string
		versionRange string
		want         string
	}{
		{"exact version", "1.0.0", "1.0.0"},
		{"exact version missing", "4.0.0", ""},
		{"caret major", "^1.0.0", "1.2.0"},
		{"caret minor", "^1.1.0", "1.2.0"},
		{"tilde", "~1.0.0", "1.0.1"},
		{"gte", ">=2.0.0", "2.1.0"},
		{"gt", ">1.2.0", "2.1.0"},
		{"lte", "<=1.1.0", "1.1.0"},
		{"lt", "<2.0.0", "1.2.0"},
		{"latest", "latest", "2.1.0"},
		{"star", "*", "2.1.0"},
		{"empty", "", "2.1.0"},
		{"x-range major", "1.x", "1.2.0"},
		{"x-range minor", "1.0.x", "1.0.1"},
		{"x-range star suffix", "1.2.*", "1.2.0"},
		{"partial major", "1", "1.2.0"},
		{"partial minor", "1.1", "1.1.0"},
		{"tilde major", "~1", "1.2.0"},
		{"tilde minor", "~1.1", "1.1.0"},
		{"hyphen range", "1.0.0 - 1.1.0", "1.1.0"},
		{"or range", "^1.0.0 || ^2.0.0", "2.1.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchVersion(versions, tt.versionRange)
			if got != tt.want {
				t.Errorf("matchVersion(%q) = %q, want %q", tt.versionRange, got, tt.want)
			}
		})
	}
}

func TestMatchCaretRange(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		base     string
		want     string
	}{
		{
			"major version constraint",
			[]string{"1.0.0", "1.1.0", "1.2.0", "2.0.0"},
			"1.0.0",
			"1.2.0",
		},
		{
			"zero major",
			[]string{"0.1.0", "0.1.1", "0.2.0"},
			"0.1.0",
			"0.1.1",
		},
		{
			"zero major zero minor",
			[]string{"0.0.1", "0.0.2", "0.1.0"},
			"0.0.1",
			"0.0.1",
		},
		{
			"zero major zero minor higher base",
			[]string{"0.0.1", "0.0.2", "0.0.3", "0.1.0"},
			"0.0.2",
			"0.0.2",
		},
		{
			"zero major zero minor without patch",
			[]string{"0.0.1", "0.0.2", "0.1.0"},
			"0.0",
			"0.0.2",
		},
		{
			"zero major only",
			[]string{"0.0.1", "0.1.0", "0.2.3", "1.0.0"},
			"0",
			"0.2.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchCaretRange(tt.versions, tt.base)
			if got != tt.want {
				t.Errorf("matchCaretRange(%v, %q) = %q, want %q", tt.versions, tt.base, got, tt.want)
			}
		})
	}
}

func TestMatchTildeRange(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		base     string
		want     string
	}{
		{
			"patch updates only",
			[]string{"1.0.0", "1.0.1", "1.0.2", "1.1.0"},
			"1.0.0",
			"1.0.2",
		},
		{
			"no match",
			[]string{"1.1.0", "1.2.0"},
			"1.0.0",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchTildeRange(tt.versions, tt.base)
			if got != tt.want {
				t.Errorf("matchTildeRange(%v, %q) = %q, want %q", tt.versions, tt.base, got, tt.want)
			}
		})
	}
}

func TestRegistryResolveVersion(t *testing.T) {
	litRegistry := testutil.LoadFixtureFile(t, "lit_registry.json")

	mockFetcher := NewMockFetcher()
	mockFetcher.AddResponse("https://registry.npmjs.org/lit", litRegistry)

	registry := NewRegistry(mockFetcher)
	ctx := context.Background()

	tests := []struct {
		name         string
		pkgName      string
		versionRange string
		want         string
		wantErr      bool
	}{
		{"dist-tag latest", "lit", "latest", "3.0.0", false},
		{"caret range", "lit", "^2.0.0", "2.1.0", false},
		{"exact version", "lit", "3.0.0", "3.0.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := registry.ResolveVersion(ctx, tt.pkgName, tt.versionRange)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveVersion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ResolveVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersionCache(t *testing.T) {
	cache := NewVersionCache()

	// Initially not in cache
	_, ok := cache.Get("lit", "^3.0.0")
	if ok {
		t.Error("Expected cache miss for new entry")
	}

	// Set and get
	cache.Set("lit", "^3.0.0", "3.1.0")
	version, ok := cache.Get("lit", "^3.0.0")
	if !ok {
		t.Error("Expected cache hit after set")
	}
	if version != "3.1.0" {
		t.Errorf("Expected version 3.1.0, got %s", version)
	}
}

type countingFetcher struct {
	Fetcher
	count atomic.Int64
}

func (c *countingFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	c.count.Add(1)
	return c.Fetcher.Fetch(ctx, url)
}

func TestResolveVersionCoalescesConcurrentPackumentFetches(t *testing.T) {
	litRegistry := testutil.LoadFixtureFile(t, "lit_registry.json")
	mock := NewMockFetcher()
	mock.AddResponse("https://registry.npmjs.org/lit", litRegistry)
	blocking := &blockingFetcher{
		base:    mock,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	fetcher := &countingFetcher{Fetcher: blocking}

	registry := NewRegistry(fetcher)
	ctx := context.Background()

	// The first request registers the in-flight call and blocks inside the
	// transport, guaranteeing the remaining ranges overlap it.
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := registry.ResolveVersion(ctx, "lit", "^2.0.0"); err != nil {
			errs <- err
		}
	}()
	<-blocking.started

	for _, rng := range []string{"~3.0.0", ">=2.0.0"} {
		wg.Add(1)
		go func(rng string) {
			defer wg.Done()
			if _, err := registry.ResolveVersion(ctx, "lit", rng); err != nil {
				errs <- err
			}
		}(rng)
	}

	time.Sleep(20 * time.Millisecond)
	close(blocking.release)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("ResolveVersion error: %v", err)
	}

	if got := fetcher.count.Load(); got != 1 {
		t.Errorf("expected one packument fetch for concurrent ranges, got %d", got)
	}
}

// blockingFetcher blocks the first Fetch until release is closed.
type blockingFetcher struct {
	base    Fetcher
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (f *blockingFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	f.once.Do(func() { close(f.started) })
	select {
	case <-f.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return f.base.Fetch(ctx, url)
}

func TestResolveVersionRetriesPackumentAfterFailure(t *testing.T) {
	litRegistry := testutil.LoadFixtureFile(t, "lit_registry.json")
	mock := NewMockFetcher()
	var calls atomic.Int32
	fetcher := &flakyFetcher{
		base: mock,
		fail: func(url string) bool {
			return url == "https://registry.npmjs.org/lit" && calls.Add(1) == 1
		},
	}
	mock.AddResponse("https://registry.npmjs.org/lit", litRegistry)

	registry := NewRegistry(fetcher)
	ctx := context.Background()

	if _, err := registry.ResolveVersion(ctx, "lit", "^2.0.0"); err == nil {
		t.Fatal("expected first packument fetch to fail")
	}
	got, err := registry.ResolveVersion(ctx, "lit", "^2.0.0")
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if got != "2.1.0" {
		t.Errorf("got %q, want 2.1.0", got)
	}
}

// contextBoundFetcher blocks the first request until its context is canceled,
// then answers subsequent requests from base.
type contextBoundFetcher struct {
	base     Fetcher
	started  chan struct{}
	once     sync.Once
	fetchCnt atomic.Int32
}

func (f *contextBoundFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	if f.fetchCnt.Add(1) == 1 {
		f.once.Do(func() { close(f.started) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.base.Fetch(ctx, url)
}

func TestResolveVersionRetriesCoalescedFetchAfterLeaderCancel(t *testing.T) {
	litRegistry := testutil.LoadFixtureFile(t, "lit_registry.json")
	mock := NewMockFetcher()
	mock.AddResponse("https://registry.npmjs.org/lit", litRegistry)

	fetcher := &contextBoundFetcher{base: mock, started: make(chan struct{})}
	registry := NewRegistry(fetcher)

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	defer cancelLeader()

	leaderDone := make(chan error, 1)
	go func() {
		_, err := registry.ResolveVersion(leaderCtx, "lit", "^2.0.0")
		leaderDone <- err
	}()

	<-fetcher.started

	// Join the in-flight fetch as a second caller with a live context.
	waiterDone := make(chan struct {
		version string
		err     error
	}, 1)
	go func() {
		version, err := registry.ResolveVersion(context.Background(), "lit", "^2.0.0")
		waiterDone <- struct {
			version string
			err     error
		}{version, err}
	}()

	// Give the waiter time to attach to the in-flight call before the leader
	// is canceled.
	time.Sleep(50 * time.Millisecond)
	cancelLeader()

	if err := <-leaderDone; err == nil {
		t.Error("expected the canceled leader to fail")
	}

	got := <-waiterDone
	if got.err != nil {
		t.Fatalf("expected the waiter to retry with its own context, got %v", got.err)
	}
	if got.version != "2.1.0" {
		t.Errorf("got version %q, want 2.1.0", got.version)
	}
}

func TestRegistrySendsPackumentAcceptHeader(t *testing.T) {
	litRegistry := testutil.LoadFixtureFile(t, "lit_registry.json")
	var gotAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(litRegistry)
	}))
	defer server.Close()

	registry := NewRegistryWithURL(NewHTTPFetcher(), server.URL)
	if _, err := registry.ResolveVersion(context.Background(), "lit", "^2.0.0"); err != nil {
		t.Fatalf("ResolveVersion error: %v", err)
	}
	if gotAccept != packumentAccept {
		t.Errorf("Accept header = %q, want %q", gotAccept, packumentAccept)
	}
}

// flakyFetcher fails matching requests according to fail.
type flakyFetcher struct {
	base Fetcher
	fail func(url string) bool
}

func (f *flakyFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	if f.fail(url) {
		return nil, &FetchError{URL: url, StatusCode: 500, Message: "transient"}
	}
	return f.base.Fetch(ctx, url)
}

func TestResolveVersionSkipsRegistryForExact(t *testing.T) {
	litRegistry := testutil.LoadFixtureFile(t, "lit_registry.json")
	mock := NewMockFetcher()
	mock.AddResponse("https://registry.npmjs.org/lit", litRegistry)
	fetcher := &countingFetcher{Fetcher: mock}

	registry := NewRegistry(fetcher)
	ctx := context.Background()

	// An exact version needs no registry metadata.
	got, err := registry.ResolveVersion(ctx, "lit", "3.0.0")
	if err != nil {
		t.Fatalf("ResolveVersion exact failed: %v", err)
	}
	if got != "3.0.0" {
		t.Errorf("ResolveVersion exact = %q", got)
	}
	if fetcher.count.Load() != 0 {
		t.Errorf("expected no registry fetch for exact version, got %d", fetcher.count.Load())
	}

	// Resolving a range uses the registry once; a later exact lookup for the
	// resolved version is answered by the exact-version short circuit, not by
	// a cache entry seeded during range resolution.
	got, err = registry.ResolveVersion(ctx, "lit", "^2.0.0")
	if err != nil {
		t.Fatalf("ResolveVersion range failed: %v", err)
	}
	before := fetcher.count.Load()
	if before != 1 {
		t.Errorf("expected one registry fetch, got %d", before)
	}
	got, err = registry.ResolveVersion(ctx, "lit", got)
	if err != nil {
		t.Fatalf("ResolveVersion exact after range failed: %v", err)
	}
	if got != "2.1.0" {
		t.Errorf("ResolveVersion exact after range = %q, want 2.1.0", got)
	}
	if fetcher.count.Load() != before {
		t.Errorf("expected exact short circuit to avoid a fetch, got %d fetches", fetcher.count.Load())
	}
}

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

package options

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	mappacdn "bennypowers.dev/mappa/cdn"
	"bennypowers.dev/mappa/packagejson"
	cdnresolver "bennypowers.dev/mappa/resolve/cdn"
)

// deadlineFetcher records whether requests carry a context deadline and then
// fails with a 404.
type deadlineFetcher struct {
	sawDeadline atomic.Bool
	mu          sync.Mutex
	urls        []string
}

func (f *deadlineFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	if _, ok := ctx.Deadline(); ok {
		f.sawDeadline.Store(true)
	}
	f.mu.Lock()
	f.urls = append(f.urls, url)
	f.mu.Unlock()
	return nil, &mappacdn.FetchError{URL: url, StatusCode: 404, Message: "Not Found"}
}

func (f *deadlineFetcher) firstURL() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.urls) == 0 {
		return ""
	}
	return f.urls[0]
}

func intPtr(n int) *int { return &n }

func TestApplyWiresTimeout(t *testing.T) {
	tests := []struct {
		name         string
		timeoutMs    *int
		wantDeadline bool
	}{
		{name: "positive timeout reaches resolver", timeoutMs: intPtr(50), wantDeadline: true},
		{name: "zero disables timeout", timeoutMs: intPtr(0), wantDeadline: false},
		{name: "nil keeps default", timeoutMs: nil, wantDeadline: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetcher := &deadlineFetcher{}
			resolver := cdnresolver.New(fetcher)
			got, err := Generate{TimeoutMs: tt.timeoutMs}.Apply(resolver)
			if err != nil {
				t.Fatalf("Apply error: %v", err)
			}

			pkg := &packagejson.PackageJSON{
				Dependencies: map[string]string{"dep": "1.0.0"},
			}
			_, _ = got.ResolvePackageJSON(context.Background(), pkg)

			if fetcher.sawDeadline.Load() != tt.wantDeadline {
				t.Errorf("sawDeadline = %v, want %v", fetcher.sawDeadline.Load(), tt.wantDeadline)
			}
		})
	}
}

func TestApplyWiresAllowPartial(t *testing.T) {
	fetcher := &deadlineFetcher{}
	resolver, err := Generate{AllowPartial: true}.Apply(cdnresolver.New(fetcher))
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	pkg := &packagejson.PackageJSON{
		Dependencies: map[string]string{"missing": "^1.0.0"},
	}
	im, err := resolver.ResolvePackageJSON(context.Background(), pkg)
	if err != nil {
		t.Fatalf("expected allowPartial to suppress the error, got %v", err)
	}
	if len(im.Warnings) != 1 {
		t.Fatalf("expected one warning, got %v", im.Warnings)
	}
	if !strings.Contains(im.Warnings[0], "missing@^1.0.0") {
		t.Errorf("unexpected warning %q", im.Warnings[0])
	}
}

func TestApplyWiresProviderAndTemplateValidation(t *testing.T) {
	fetcher := &deadlineFetcher{}
	resolver, err := Generate{CDN: "unpkg"}.Apply(cdnresolver.New(fetcher))
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	pkg := &packagejson.PackageJSON{
		Dependencies: map[string]string{"dep": "1.0.0"},
	}
	_, _ = resolver.ResolvePackageJSON(context.Background(), pkg)
	if got := fetcher.firstURL(); !strings.HasPrefix(got, "https://unpkg.com/dep@1.0.0/") {
		t.Errorf("expected provider URL to be unpkg, got %q", got)
	}

	if _, err := (Generate{Template: "{bogus}"}).Apply(cdnresolver.New(&deadlineFetcher{})); err == nil {
		t.Error("expected invalid template to fail")
	}
}

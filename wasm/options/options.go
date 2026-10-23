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

// Package options translates the JavaScript-facing generate options into
// resolver configuration. It is deliberately free of syscall/js so the
// mapping can be unit tested on every platform.
package options

import (
	"fmt"
	"strings"
	"time"

	mappacdn "bennypowers.dev/mappa/cdn"
	cdnresolver "bennypowers.dev/mappa/resolve/cdn"
)

// Generate mirrors the options accepted by the JavaScript generate() API.
type Generate struct {
	// CDN is the CDN provider name ("esm.sh", "unpkg", "jsdelivr").
	CDN string
	// Template is a custom CDN URL template.
	Template string
	// Conditions are the export conditions to resolve.
	Conditions []string
	// Exclude lists packages to omit, including as transitive dependencies.
	Exclude []string
	// TimeoutMs is the per-request timeout in milliseconds. A nil value keeps
	// the resolver default (30s); zero disables the timeout.
	TimeoutMs *int
	// AllowPartial returns the partial import map plus warnings instead of an
	// error when some dependencies fail to resolve.
	AllowPartial bool
}

// Apply returns a resolver configured according to the options.
func (o Generate) Apply(resolver *cdnresolver.Resolver) (*cdnresolver.Resolver, error) {
	if o.CDN != "" {
		provider := mappacdn.ProviderByName(o.CDN)
		if provider == nil {
			return nil, fmt.Errorf("unknown CDN provider %q (supported: %s)",
				o.CDN, strings.Join(mappacdn.ProviderNames(), ", "))
		}
		resolver = resolver.WithProvider(*provider)
	}

	if o.Template != "" {
		var err error
		resolver, err = resolver.WithTemplate(o.Template)
		if err != nil {
			return nil, fmt.Errorf("invalid template: %w", err)
		}
	}

	if len(o.Conditions) > 0 {
		resolver = resolver.WithConditions(o.Conditions)
	}

	if len(o.Exclude) > 0 {
		resolver = resolver.WithExclude(o.Exclude)
	}

	if o.TimeoutMs != nil {
		resolver = resolver.WithRequestTimeout(time.Duration(*o.TimeoutMs) * time.Millisecond)
	}

	if o.AllowPartial {
		resolver = resolver.WithAllowPartial(true)
	}

	return resolver, nil
}

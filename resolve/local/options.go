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

package local

import (
	"fmt"

	"bennypowers.dev/mappa/importmap"
)

// Options configures a local Resolver from external callers (CLI, WASM, etc).
type Options struct {
	Template        string
	Conditions      []string
	IncludePackages []string
	Exclude         []string
	InputMap        *importmap.ImportMap
}

// Apply configures the given Resolver with these options, returning the result.
func (o *Options) Apply(r *Resolver) (*Resolver, error) {
	if o == nil {
		return r, nil
	}
	if o.Template != "" {
		var err error
		r, err = r.WithTemplate(o.Template)
		if err != nil {
			return nil, fmt.Errorf("invalid template: %w", err)
		}
	}
	if len(o.Conditions) > 0 {
		r = r.WithConditions(o.Conditions)
	}
	if len(o.IncludePackages) > 0 {
		r = r.WithPackages(o.IncludePackages)
	}
	if len(o.Exclude) > 0 {
		r = r.WithExclude(o.Exclude)
	}
	if o.InputMap != nil {
		r = r.WithInputMap(o.InputMap)
	}
	return r, nil
}

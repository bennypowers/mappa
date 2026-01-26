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
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Registry provides access to the npm registry for package metadata.
type Registry struct {
	fetcher      Fetcher
	baseURL      string
	versionCache *VersionCache
}

// RegistryPackage represents package metadata from the npm registry.
type RegistryPackage struct {
	Name     string                    `json:"name"`
	DistTags map[string]string         `json:"dist-tags"`
	Versions map[string]RegistryVersion `json:"versions"`
}

// RegistryVersion represents a specific version's metadata.
type RegistryVersion struct {
	Version      string            `json:"version"`
	Dependencies map[string]string `json:"dependencies"`
}

// VersionCache caches resolved versions to avoid repeated registry lookups.
type VersionCache struct {
	mu      sync.RWMutex
	entries map[string]string // pkgName@range -> resolved version
}

// NewRegistry creates a new npm registry client.
func NewRegistry(fetcher Fetcher) *Registry {
	return &Registry{
		fetcher:      fetcher,
		baseURL:      "https://registry.npmjs.org",
		versionCache: NewVersionCache(),
	}
}

// NewRegistryWithURL creates a new registry client with a custom registry URL.
func NewRegistryWithURL(fetcher Fetcher, baseURL string) *Registry {
	return &Registry{
		fetcher:      fetcher,
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		versionCache: NewVersionCache(),
	}
}

// NewVersionCache creates a new version cache.
func NewVersionCache() *VersionCache {
	return &VersionCache{
		entries: make(map[string]string),
	}
}

// Get retrieves a cached version resolution.
func (c *VersionCache) Get(pkgName, versionRange string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	key := pkgName + "@" + versionRange
	version, ok := c.entries[key]
	return version, ok
}

// Set stores a version resolution in the cache.
func (c *VersionCache) Set(pkgName, versionRange, resolvedVersion string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := pkgName + "@" + versionRange
	c.entries[key] = resolvedVersion
}

// ResolveVersion resolves a semver range to a specific version.
func (r *Registry) ResolveVersion(ctx context.Context, pkgName, versionRange string) (string, error) {
	// Check cache first
	if cached, ok := r.versionCache.Get(pkgName, versionRange); ok {
		return cached, nil
	}

	// Fetch package metadata from registry
	url := fmt.Sprintf("%s/%s", r.baseURL, pkgName)
	data, err := r.fetcher.Fetch(ctx, url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch package %s: %w", pkgName, err)
	}

	var pkg RegistryPackage
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "", fmt.Errorf("failed to parse package metadata for %s: %w", pkgName, err)
	}

	// Resolve the version
	resolved, err := resolveVersionFromPackage(&pkg, versionRange)
	if err != nil {
		return "", err
	}

	// Cache the result
	r.versionCache.Set(pkgName, versionRange, resolved)
	return resolved, nil
}

// Dependencies returns the dependencies for a specific version of a package.
func (r *Registry) Dependencies(ctx context.Context, pkgName, version string) (map[string]string, error) {
	url := fmt.Sprintf("%s/%s/%s", r.baseURL, pkgName, version)
	data, err := r.fetcher.Fetch(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch package %s@%s: %w", pkgName, version, err)
	}

	var ver RegistryVersion
	if err := json.Unmarshal(data, &ver); err != nil {
		return nil, fmt.Errorf("failed to parse version metadata: %w", err)
	}

	return ver.Dependencies, nil
}

// resolveVersionFromPackage resolves a version range from package metadata.
func resolveVersionFromPackage(pkg *RegistryPackage, versionRange string) (string, error) {
	// Handle dist-tags (latest, next, etc.)
	if tag, ok := pkg.DistTags[versionRange]; ok {
		return tag, nil
	}

	// Handle exact version
	if _, ok := pkg.Versions[versionRange]; ok {
		return versionRange, nil
	}

	// Collect and sort all versions
	versions := make([]string, 0, len(pkg.Versions))
	for v := range pkg.Versions {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool {
		return compareSemver(versions[i], versions[j]) < 0
	})

	// Find the best matching version
	matched := matchVersion(versions, versionRange)
	if matched == "" {
		return "", fmt.Errorf("no version matching %q found for package %s", versionRange, pkg.Name)
	}

	return matched, nil
}

// SemVer represents a parsed semantic version.
type SemVer struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
}

var semverPattern = regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:-(.+))?$`)

// parseSemver parses a semantic version string.
func parseSemver(version string) (*SemVer, error) {
	matches := semverPattern.FindStringSubmatch(version)
	if matches == nil {
		return nil, fmt.Errorf("invalid semver: %s", version)
	}

	sv := &SemVer{}
	sv.Major, _ = strconv.Atoi(matches[1])
	if matches[2] != "" {
		sv.Minor, _ = strconv.Atoi(matches[2])
	}
	if matches[3] != "" {
		sv.Patch, _ = strconv.Atoi(matches[3])
	}
	sv.Prerelease = matches[4]

	return sv, nil
}

// compareSemver compares two semver strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareSemver(a, b string) int {
	av, err := parseSemver(a)
	if err != nil {
		return -1
	}
	bv, err := parseSemver(b)
	if err != nil {
		return 1
	}

	if av.Major != bv.Major {
		if av.Major < bv.Major {
			return -1
		}
		return 1
	}
	if av.Minor != bv.Minor {
		if av.Minor < bv.Minor {
			return -1
		}
		return 1
	}
	if av.Patch != bv.Patch {
		if av.Patch < bv.Patch {
			return -1
		}
		return 1
	}

	// Prerelease versions are lower precedence than release versions
	if av.Prerelease != "" && bv.Prerelease == "" {
		return -1
	}
	if av.Prerelease == "" && bv.Prerelease != "" {
		return 1
	}
	if av.Prerelease != bv.Prerelease {
		if av.Prerelease < bv.Prerelease {
			return -1
		}
		return 1
	}

	return 0
}

// matchVersion finds the best version matching a semver range.
func matchVersion(versions []string, versionRange string) string {
	versionRange = strings.TrimSpace(versionRange)

	// Handle "latest" specially
	if versionRange == "latest" || versionRange == "" || versionRange == "*" {
		// Return highest non-prerelease version
		for i := len(versions) - 1; i >= 0; i-- {
			sv, err := parseSemver(versions[i])
			if err == nil && sv.Prerelease == "" {
				return versions[i]
			}
		}
		if len(versions) > 0 {
			return versions[len(versions)-1]
		}
		return ""
	}

	// Handle || (union) ranges - must check before prefix operators
	if strings.Contains(versionRange, "||") {
		return matchOrRange(versions, versionRange)
	}

	// Handle caret ranges (^1.2.3)
	if base, ok := strings.CutPrefix(versionRange, "^"); ok {
		return matchCaretRange(versions, base)
	}

	// Handle tilde ranges (~1.2.3)
	if base, ok := strings.CutPrefix(versionRange, "~"); ok {
		return matchTildeRange(versions, base)
	}

	// Handle >= ranges
	if base, ok := strings.CutPrefix(versionRange, ">="); ok {
		return matchGteRange(versions, base)
	}

	// Handle > ranges
	if base, ok := strings.CutPrefix(versionRange, ">"); ok {
		return matchGtRange(versions, base)
	}

	// Handle <= ranges
	if base, ok := strings.CutPrefix(versionRange, "<="); ok {
		return matchLteRange(versions, base)
	}

	// Handle < ranges
	if base, ok := strings.CutPrefix(versionRange, "<"); ok {
		return matchLtRange(versions, base)
	}

	// Handle exact version with = prefix
	if exact, ok := strings.CutPrefix(versionRange, "="); ok {
		if slices.Contains(versions, exact) {
			return exact
		}
		return ""
	}

	// Handle x-ranges (1.x, 1.2.x)
	if strings.Contains(versionRange, "x") || strings.Contains(versionRange, "X") {
		return matchXRange(versions, versionRange)
	}

	// Handle hyphen ranges (1.0.0 - 2.0.0)
	if strings.Contains(versionRange, " - ") {
		return matchHyphenRange(versions, versionRange)
	}

	// Handle space-separated ranges (intersection)
	if strings.Contains(versionRange, " ") && !strings.Contains(versionRange, "||") {
		parts := strings.Fields(versionRange)
		candidates := versions
		for _, part := range parts {
			var filtered []string
			for _, v := range candidates {
				if versionSatisfies(v, part) {
					filtered = append(filtered, v)
				}
			}
			candidates = filtered
		}
		if len(candidates) > 0 {
			return candidates[len(candidates)-1]
		}
		return ""
	}

	// Treat as exact version
	if slices.Contains(versions, versionRange) {
		return versionRange
	}

	return ""
}

// matchCaretRange matches versions for ^major.minor.patch
// Allows changes that do not modify the left-most non-zero element.
func matchCaretRange(versions []string, baseVersion string) string {
	base, err := parseSemver(baseVersion)
	if err != nil {
		return ""
	}

	var matches []string
	for _, v := range versions {
		sv, err := parseSemver(v)
		if err != nil || sv.Prerelease != "" {
			continue
		}

		// ^0.0.x - only matches 0.0.x
		if base.Major == 0 && base.Minor == 0 {
			if sv.Major == 0 && sv.Minor == 0 && sv.Patch >= base.Patch {
				matches = append(matches, v)
			}
			continue
		}

		// ^0.x.y - allows 0.x.z where z >= y (same minor, patch can increase)
		if base.Major == 0 {
			if sv.Major == 0 && sv.Minor == base.Minor && sv.Patch >= base.Patch {
				matches = append(matches, v)
			}
			continue
		}

		// ^x.y.z - allows x.*.* where major is same
		if sv.Major == base.Major && compareSemver(v, baseVersion) >= 0 {
			matches = append(matches, v)
		}
	}

	if len(matches) > 0 {
		return matches[len(matches)-1]
	}
	return ""
}

// matchTildeRange matches versions for ~major.minor.patch
// Allows patch-level changes.
func matchTildeRange(versions []string, baseVersion string) string {
	base, err := parseSemver(baseVersion)
	if err != nil {
		return ""
	}

	var matches []string
	for _, v := range versions {
		sv, err := parseSemver(v)
		if err != nil || sv.Prerelease != "" {
			continue
		}

		// Must match major.minor, patch can be >=
		if sv.Major == base.Major && sv.Minor == base.Minor && sv.Patch >= base.Patch {
			matches = append(matches, v)
		}
	}

	if len(matches) > 0 {
		return matches[len(matches)-1]
	}
	return ""
}

// matchGteRange matches versions >= a given version.
func matchGteRange(versions []string, baseVersion string) string {
	baseVersion = strings.TrimSpace(baseVersion)
	var matches []string
	for _, v := range versions {
		sv, err := parseSemver(v)
		if err != nil || sv.Prerelease != "" {
			continue
		}
		if compareSemver(v, baseVersion) >= 0 {
			matches = append(matches, v)
		}
	}
	if len(matches) > 0 {
		return matches[len(matches)-1]
	}
	return ""
}

// matchGtRange matches versions > a given version.
func matchGtRange(versions []string, baseVersion string) string {
	baseVersion = strings.TrimSpace(baseVersion)
	var matches []string
	for _, v := range versions {
		sv, err := parseSemver(v)
		if err != nil || sv.Prerelease != "" {
			continue
		}
		if compareSemver(v, baseVersion) > 0 {
			matches = append(matches, v)
		}
	}
	if len(matches) > 0 {
		return matches[len(matches)-1]
	}
	return ""
}

// matchLteRange matches versions <= a given version.
func matchLteRange(versions []string, baseVersion string) string {
	baseVersion = strings.TrimSpace(baseVersion)
	var matches []string
	for _, v := range versions {
		sv, err := parseSemver(v)
		if err != nil || sv.Prerelease != "" {
			continue
		}
		if compareSemver(v, baseVersion) <= 0 {
			matches = append(matches, v)
		}
	}
	if len(matches) > 0 {
		return matches[len(matches)-1]
	}
	return ""
}

// matchLtRange matches versions < a given version.
func matchLtRange(versions []string, baseVersion string) string {
	baseVersion = strings.TrimSpace(baseVersion)
	var matches []string
	for _, v := range versions {
		sv, err := parseSemver(v)
		if err != nil || sv.Prerelease != "" {
			continue
		}
		if compareSemver(v, baseVersion) < 0 {
			matches = append(matches, v)
		}
	}
	if len(matches) > 0 {
		return matches[len(matches)-1]
	}
	return ""
}

// matchXRange matches x-range versions (1.x, 1.2.x)
func matchXRange(versions []string, pattern string) string {
	pattern = strings.ToLower(pattern)
	parts := strings.Split(pattern, ".")

	var matches []string
	for _, v := range versions {
		sv, err := parseSemver(v)
		if err != nil || sv.Prerelease != "" {
			continue
		}

		match := true
		for i, part := range parts {
			if part == "x" || part == "*" {
				continue
			}
			val, err := strconv.Atoi(part)
			if err != nil {
				match = false
				break
			}
			switch i {
			case 0:
				if sv.Major != val {
					match = false
				}
			case 1:
				if sv.Minor != val {
					match = false
				}
			case 2:
				if sv.Patch != val {
					match = false
				}
			}
		}
		if match {
			matches = append(matches, v)
		}
	}

	if len(matches) > 0 {
		return matches[len(matches)-1]
	}
	return ""
}

// matchHyphenRange matches hyphen ranges (1.0.0 - 2.0.0)
func matchHyphenRange(versions []string, rangeStr string) string {
	parts := strings.Split(rangeStr, " - ")
	if len(parts) != 2 {
		return ""
	}
	lower := strings.TrimSpace(parts[0])
	upper := strings.TrimSpace(parts[1])

	var matches []string
	for _, v := range versions {
		sv, err := parseSemver(v)
		if err != nil || sv.Prerelease != "" {
			continue
		}
		if compareSemver(v, lower) >= 0 && compareSemver(v, upper) <= 0 {
			matches = append(matches, v)
		}
	}
	if len(matches) > 0 {
		return matches[len(matches)-1]
	}
	return ""
}

// matchOrRange matches || separated ranges
func matchOrRange(versions []string, rangeStr string) string {
	parts := strings.Split(rangeStr, "||")
	var allMatches []string
	for _, part := range parts {
		matched := matchVersion(versions, strings.TrimSpace(part))
		if matched != "" {
			allMatches = append(allMatches, matched)
		}
	}
	if len(allMatches) > 0 {
		// Return the highest matching version
		sort.Slice(allMatches, func(i, j int) bool {
			return compareSemver(allMatches[i], allMatches[j]) < 0
		})
		return allMatches[len(allMatches)-1]
	}
	return ""
}

// versionSatisfies checks if a version satisfies a simple constraint.
func versionSatisfies(version, constraint string) bool {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" || constraint == "*" {
		return true
	}

	if base, ok := strings.CutPrefix(constraint, ">="); ok {
		return compareSemver(version, strings.TrimSpace(base)) >= 0
	}
	if base, ok := strings.CutPrefix(constraint, ">"); ok {
		return compareSemver(version, strings.TrimSpace(base)) > 0
	}
	if base, ok := strings.CutPrefix(constraint, "<="); ok {
		return compareSemver(version, strings.TrimSpace(base)) <= 0
	}
	if base, ok := strings.CutPrefix(constraint, "<"); ok {
		return compareSemver(version, strings.TrimSpace(base)) < 0
	}
	if base, ok := strings.CutPrefix(constraint, "^"); ok {
		return matchCaretRange([]string{version}, base) != ""
	}
	if base, ok := strings.CutPrefix(constraint, "~"); ok {
		return matchTildeRange([]string{version}, base) != ""
	}
	if base, ok := strings.CutPrefix(constraint, "="); ok {
		return version == strings.TrimSpace(base)
	}

	return version == constraint
}

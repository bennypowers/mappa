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
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// packumentAccept asks the registry for the abbreviated packument format.
// It is significantly smaller than the full document and contains everything
// mappa needs (versions and dist-tags).
const packumentAccept = "application/vnd.npm.install-v1+json"

// maxInflightPackuments bounds the number of packument fetches that can be
// coalesced at once. Beyond this, requests proceed independently.
const maxInflightPackuments = 64

// Registry provides access to the npm registry for package metadata.
type Registry struct {
	fetcher      Fetcher
	baseURL      string
	versionCache *VersionCache

	inflightMu sync.Mutex
	inflight   map[string]*packumentCall
}

// packumentCall represents one in-flight packument fetch shared by concurrent
// callers interested in the same package.
type packumentCall struct {
	done chan struct{}
	pkg  *RegistryPackage
	err  error
}

// RegistryPackage represents package metadata from the npm registry.
type RegistryPackage struct {
	Name     string                     `json:"name"`
	DistTags map[string]string          `json:"dist-tags"`
	Versions map[string]RegistryVersion `json:"versions"`
}

// RegistryVersion represents a specific version's metadata.
type RegistryVersion struct {
	Version      string            `json:"version"`
	Dependencies map[string]string `json:"dependencies"`
}

// VersionCache caches resolved versions to avoid repeated registry lookups.
// Has a maximum size with LRU eviction to prevent unbounded memory growth.
type VersionCache struct {
	lru *lru[string]
}

// NewRegistry creates a new npm registry client.
func NewRegistry(fetcher Fetcher) *Registry {
	return &Registry{
		fetcher:      fetcher,
		baseURL:      "https://registry.npmjs.org",
		versionCache: NewVersionCache(),
		inflight:     make(map[string]*packumentCall),
	}
}

// NewRegistryWithURL creates a new registry client with a custom registry URL.
func NewRegistryWithURL(fetcher Fetcher, baseURL string) *Registry {
	return &Registry{
		fetcher:      fetcher,
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		versionCache: NewVersionCache(),
		inflight:     make(map[string]*packumentCall),
	}
}

// NewVersionCache creates a new version cache with default max size.
func NewVersionCache() *VersionCache {
	return NewVersionCacheWithSize(1000)
}

// NewVersionCacheWithSize creates a new version cache with the specified max size.
func NewVersionCacheWithSize(maxSize int) *VersionCache {
	if maxSize <= 0 {
		maxSize = 1000
	}
	return &VersionCache{lru: newLRU[string](maxSize)}
}

// Get retrieves a cached version resolution.
func (c *VersionCache) Get(pkgName, versionRange string) (string, bool) {
	return c.lru.get(pkgName + "@" + versionRange)
}

// Set stores a version resolution in the cache.
func (c *VersionCache) Set(pkgName, versionRange, resolvedVersion string) {
	c.lru.set(pkgName+"@"+versionRange, resolvedVersion)
}

// ResolveVersion resolves a semver range to a specific version.
func (r *Registry) ResolveVersion(ctx context.Context, pkgName, versionRange string) (string, error) {
	// Check cache first
	if cached, ok := r.versionCache.Get(pkgName, versionRange); ok {
		return cached, nil
	}

	// An exact version needs no registry lookup. This also means exact
	// versions never reach resolveVersionFromPackage, so it needs no exact
	// match branch of its own.
	if isExactVersion(versionRange) {
		return strings.TrimPrefix(strings.TrimSpace(versionRange), "v"), nil
	}

	pkg, err := r.fetchPackument(ctx, pkgName)
	if err != nil {
		return "", fmt.Errorf("failed to fetch package %s: %w", pkgName, err)
	}

	// Resolve the version
	resolved, err := resolveVersionFromPackage(pkg, versionRange)
	if err != nil {
		return "", err
	}

	r.versionCache.Set(pkgName, versionRange, resolved)
	return resolved, nil
}

// fetchPackument fetches and parses package metadata, coalescing concurrent
// requests for the same package into a single registry call. Failures are not
// cached, so a later call retries.
func (r *Registry) fetchPackument(ctx context.Context, pkgName string) (*RegistryPackage, error) {
	r.inflightMu.Lock()
	if call, ok := r.inflight[pkgName]; ok {
		r.inflightMu.Unlock()
		select {
		case <-call.done:
			// The leader's context may have died while ours is still live. The
			// failure belongs to the leader's request, not to this caller, so
			// retry under our own context instead of propagating it.
			if ctx.Err() == nil && isContextError(call.err) {
				return r.fetchPackument(ctx, pkgName)
			}
			return call.pkg, call.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Only coalesce while the in-flight map has room. Beyond the bound,
	// requests proceed independently rather than blocking on each other.
	if len(r.inflight) < maxInflightPackuments {
		call := &packumentCall{done: make(chan struct{})}
		r.inflight[pkgName] = call
		r.inflightMu.Unlock()

		pkg, err := r.doFetchPackument(ctx, pkgName)
		call.pkg, call.err = pkg, err
		r.inflightMu.Lock()
		delete(r.inflight, pkgName)
		r.inflightMu.Unlock()
		close(call.done)
		return pkg, err
	}
	r.inflightMu.Unlock()

	return r.doFetchPackument(ctx, pkgName)
}

func (r *Registry) doFetchPackument(ctx context.Context, pkgName string) (*RegistryPackage, error) {
	url := fmt.Sprintf("%s/%s", r.baseURL, pkgName)
	data, err := fetchWithAccept(ctx, r.fetcher, url, packumentAccept)
	if err != nil {
		return nil, err
	}

	var pkg RegistryPackage
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("failed to parse package metadata for %s: %w", pkgName, err)
	}
	return &pkg, nil
}

// isContextError reports whether err was caused by a canceled or expired
// context rather than by the remote server.
func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// fetchWithAccept sends a media type hint when the fetcher supports headers.
func fetchWithAccept(ctx context.Context, fetcher Fetcher, url, accept string) ([]byte, error) {
	if hf, ok := fetcher.(HeaderFetcher); ok {
		return hf.FetchWithHeaders(ctx, url, map[string]string{"Accept": accept})
	}
	return fetcher.Fetch(ctx, url)
}

// exactVersionPattern matches complete semantic versions.
var exactVersionPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

// partialVersionPattern matches "1" or "1.2" style x-ranges.
var partialVersionPattern = regexp.MustCompile(`^v?\d+(?:\.\d+)?$`)

// isExactVersion reports whether s is a complete, concrete semantic version.
func isExactVersion(s string) bool {
	return exactVersionPattern.MatchString(strings.TrimSpace(s))
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

	// Exact versions never reach this function: ResolveVersion short-circuits
	// them before fetching a packument.

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
// Returns 0 if either version cannot be parsed.
func compareSemver(a, b string) int {
	av, err := parseSemver(a)
	if err != nil {
		return 0
	}
	bv, err := parseSemver(b)
	if err != nil {
		return 0
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
		return comparePrereleases(av.Prerelease, bv.Prerelease)
	}

	return 0
}

// comparePrereleases compares two prerelease strings according to SemVer 2.0.0.
// Identifiers are split by '.' and compared: numeric identifiers are compared as integers,
// non-numeric identifiers are compared lexically, numeric has lower precedence than non-numeric,
// and shorter identifier lists have lower precedence when all preceding identifiers are equal.
func comparePrereleases(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		aPart := aParts[i]
		bPart := bParts[i]

		aNum, aIsNum := parseNum(aPart)
		bNum, bIsNum := parseNum(bPart)

		if aIsNum && bIsNum {
			// Both numeric: compare as integers
			if aNum != bNum {
				if aNum < bNum {
					return -1
				}
				return 1
			}
		} else if aIsNum {
			// Numeric has lower precedence than non-numeric
			return -1
		} else if bIsNum {
			return 1
		} else {
			// Both non-numeric: compare lexically
			if aPart != bPart {
				if aPart < bPart {
					return -1
				}
				return 1
			}
		}
	}

	// Shorter list has lower precedence
	if len(aParts) != len(bParts) {
		if len(aParts) < len(bParts) {
			return -1
		}
		return 1
	}

	return 0
}

// parseNum attempts to parse a string as a non-negative integer.
func parseNum(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
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

	// Handle whitespace-separated comparator sets before the single-prefix
	// branches, which would otherwise treat the rest of the set as part of
	// the first comparator's version.
	if isCompoundComparatorSet(versionRange) {
		return matchComparatorSet(versions, versionRange)
	}

	// Handle caret ranges (^1.2.3); x-range bases like ^1.x delegate to the
	// x-range matcher because they have no concrete version to compare.
	if base, ok := strings.CutPrefix(versionRange, "^"); ok {
		if isXRange(base) {
			return matchXRange(versions, base)
		}
		return matchCaretRange(versions, base)
	}

	// Handle tilde ranges (~1.2.3)
	if base, ok := strings.CutPrefix(versionRange, "~"); ok {
		if isXRange(base) {
			return matchXRange(versions, base)
		}
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

	// Handle x-ranges (1.x, 1.2.x, 1.2.*) and partial versions (1, 1.2)
	if strings.ContainsAny(versionRange, "xX*") {
		return matchXRange(versions, versionRange)
	}
	if partialVersionPattern.MatchString(versionRange) {
		return matchXRange(versions, versionRange)
	}

	// Handle hyphen ranges (1.0.0 - 2.0.0)
	// Use precise pattern: split and verify we get exactly 2 version-like parts
	if strings.Contains(versionRange, " - ") {
		parts := strings.Split(versionRange, " - ")
		if len(parts) == 2 {
			lower := strings.TrimSpace(parts[0])
			upper := strings.TrimSpace(parts[1])
			// Verify both parts look like versions (start with digit or 'v')
			if len(lower) > 0 && len(upper) > 0 &&
				(lower[0] >= '0' && lower[0] <= '9' || lower[0] == 'v') &&
				(upper[0] >= '0' && upper[0] <= '9' || upper[0] == 'v') {
				return matchHyphenRange(versions, versionRange)
			}
		}
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

// isXRange reports whether a range component uses an x, X, or * wildcard.
func isXRange(s string) bool {
	return strings.ContainsAny(s, "xX*")
}

// isCompoundComparatorSet reports whether r is a whitespace-separated
// intersection of comparator constraints, such as ">=1.0.0 <2.0.0". Hyphen
// ranges and dist-tags are not comparator sets.
func isCompoundComparatorSet(r string) bool {
	if strings.Contains(r, " - ") || strings.Contains(r, "||") {
		return false
	}
	fields := strings.Fields(r)
	if len(fields) < 2 {
		return false
	}
	for _, field := range fields {
		if field == "" || !strings.ContainsRune("^~><=", rune(field[0])) {
			return false
		}
	}
	return true
}

// matchComparatorSet returns the highest version satisfying every comparator
// in a whitespace-separated set.
func matchComparatorSet(versions []string, rangeStr string) string {
	parts := strings.Fields(rangeStr)
	var matches []string
	for _, v := range versions {
		sv, err := parseSemver(v)
		if err != nil || sv.Prerelease != "" {
			continue
		}
		satisfies := true
		for _, part := range parts {
			if !versionSatisfies(v, part) {
				satisfies = false
				break
			}
		}
		if satisfies {
			matches = append(matches, v)
		}
	}
	if len(matches) > 0 {
		return matches[len(matches)-1]
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

		// ^0 matches every 0.x.y: only the left-most non-zero component (the
		// major) may not change. ^0.0 / ^0.x allow any patch on the same
		// minor, while ^0.0.p pins the patch because the left-most non-zero
		// component is then the patch itself.
		if base.Major == 0 {
			switch strings.Count(strings.TrimSpace(baseVersion), ".") {
			case 0:
				if sv.Major == 0 {
					matches = append(matches, v)
				}
			case 1:
				if sv.Major == 0 && sv.Minor == base.Minor {
					matches = append(matches, v)
				}
			default:
				if sv.Major == 0 && sv.Minor == base.Minor {
					if base.Minor == 0 {
						if sv.Patch == base.Patch {
							matches = append(matches, v)
						}
					} else if sv.Patch >= base.Patch {
						matches = append(matches, v)
					}
				}
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

		// Must match major (and minor, if specified), patch can be >=
		switch strings.Count(baseVersion, ".") {
		case 0: // ~1 -> >=1.0.0 <2.0.0
			if sv.Major == base.Major {
				matches = append(matches, v)
			}
		default:
			if sv.Major == base.Major && sv.Minor == base.Minor && sv.Patch >= base.Patch {
				matches = append(matches, v)
			}
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
		if isXRange(base) {
			return matchXRange([]string{version}, base) != ""
		}
		return matchCaretRange([]string{version}, base) != ""
	}
	if base, ok := strings.CutPrefix(constraint, "~"); ok {
		if isXRange(base) {
			return matchXRange([]string{version}, base) != ""
		}
		return matchTildeRange([]string{version}, base) != ""
	}
	if base, ok := strings.CutPrefix(constraint, "="); ok {
		return version == strings.TrimSpace(base)
	}

	return version == constraint
}

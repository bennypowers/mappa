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

// Package output provides shared output utilities for mappa CLI commands.
package output

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/spf13/viper"

	"bennypowers.dev/mappa/fs"
	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/trace"
)

// WarningLogger writes warnings to a writer and retains them for inspection.
// It implements resolve.Logger.
type WarningLogger struct {
	mu       sync.Mutex
	w        io.Writer
	warnings []string
}

// NewWarningLogger creates a logger that writes to w.
func NewWarningLogger(w io.Writer) *WarningLogger {
	return &WarningLogger{w: w}
}

// Warning implements resolve.Logger.
func (l *WarningLogger) Warning(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.mu.Lock()
	l.warnings = append(l.warnings, msg)
	w := l.w
	l.mu.Unlock()
	if w != nil {
		_, _ = fmt.Fprintf(w, "Warning: %s\n", msg)
	}
}

// Debug implements resolve.Logger. Debug messages are discarded.
func (l *WarningLogger) Debug(format string, args ...any) {}

// Warnings returns a copy of the collected warnings.
func (l *WarningLogger) Warnings() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.warnings...)
}

// PrintIssues writes import validation issues as warnings.
func PrintIssues(w io.Writer, issues []trace.ImportIssue) {
	for _, issue := range issues {
		_, _ = fmt.Fprintf(w, "Warning: %s:%d\n", issue.File, issue.Line)
		_, _ = fmt.Fprintf(w, "  Import %q references %s %q\n", issue.Specifier, issue.IssueType, issue.Package)
	}
}

// PrintGraphErrors writes every graph error as a warning.
func PrintGraphErrors(w io.Writer, errs []error) {
	for _, graphErr := range errs {
		_, _ = fmt.Fprintf(w, "Warning: %v\n", graphErr)
	}
}

// FatalGraphError returns a joined error when errs contains fatal module
// tracing failures, or nil otherwise.
func FatalGraphError(errs []error) error {
	fatal, _ := trace.ClassifyGraphErrors(errs)
	if len(fatal) == 0 {
		return nil
	}
	return fmt.Errorf("%d module(s) could not be traced: %w", len(fatal), errors.Join(fatal...))
}

// PrintBatchWarnings writes validation warnings and non-fatal graph errors for
// batch results in their collected order.
func PrintBatchWarnings(w io.Writer, results []trace.BatchResult) {
	for _, result := range results {
		for _, warning := range result.Warnings {
			_, _ = fmt.Fprintf(w, "Warning: %s:%d\n", warning.File, warning.Line)
			_, _ = fmt.Fprintf(w, "  Import %q references %s %q\n", warning.Specifier, warning.IssueType, warning.Package)
		}
		for _, graphErr := range result.GraphErrors {
			_, _ = fmt.Fprintf(w, "Warning: %s: %s\n", result.File, graphErr)
		}
	}
}

// ImportMap formats and outputs an import map to stdout or a file.
// If viper's "output" flag is set, writes to that file; otherwise prints to stdout.
func ImportMap(osfs fs.FileSystem, im *importmap.ImportMap, format string) error {
	output := im.Format(format)

	if outputPath := viper.GetString("output"); outputPath != "" {
		return osfs.WriteFile(outputPath, []byte(output+"\n"), 0644)
	}
	fmt.Println(output)
	return nil
}

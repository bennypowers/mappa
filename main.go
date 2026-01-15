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

// Command mappa generates and works with ES module import maps.
package main

import (
	"errors"
	"fmt"
	"os"
	"runtime/pprof"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"bennypowers.dev/mappa/cmd/generate"
	"bennypowers.dev/mappa/cmd/inject"
	"bennypowers.dev/mappa/cmd/trace"
	"bennypowers.dev/mappa/cmd/version"
)

var (
	cpuprofile     string
	cpuprofileFile *os.File
	rootCmd        = &cobra.Command{
		Use:   "mappa",
		Short: "Generate and work with ES module import maps",
		Long:  `mappa generates ES module import maps from package.json dependencies.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if cpuprofile != "" {
				f, err := os.Create(cpuprofile)
				if err != nil {
					return fmt.Errorf("could not create CPU profile: %w", err)
				}
				cpuprofileFile = f
				if err := pprof.StartCPUProfile(f); err != nil {
					closeErr := f.Close()
					return errors.Join(
						fmt.Errorf("could not start CPU profile: %w", err),
						closeErr,
					)
				}
			}
			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			if cpuprofileFile != nil {
				pprof.StopCPUProfile()
				if err := cpuprofileFile.Close(); err != nil {
					return fmt.Errorf("closing CPU profile: %w", err)
				}
			}
			return nil
		},
	}
)

func init() {
	// Root flags (persistent across all commands)
	rootCmd.PersistentFlags().StringP("package", "p", ".", "Package directory")
	rootCmd.PersistentFlags().StringP("output", "o", "", "Output file (default: stdout)")
	rootCmd.PersistentFlags().StringVar(&cpuprofile, "cpuprofile", "", "Write CPU profile to file")

	_ = viper.BindPFlag("package", rootCmd.PersistentFlags().Lookup("package"))
	_ = viper.BindPFlag("output", rootCmd.PersistentFlags().Lookup("output"))

	// Add commands
	rootCmd.AddCommand(generate.Cmd)
	rootCmd.AddCommand(trace.Cmd)
	rootCmd.AddCommand(version.Cmd)
	rootCmd.AddCommand(inject.Cmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

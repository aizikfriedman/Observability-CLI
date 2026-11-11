package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version holds the current build version of ocli.
// Overridden at release build time via -ldflags="-X ocli/cmd.Version=vX.Y.Z".
var Version = "dev"

var versionCmd = &cobra.Command{
	Use:     "version",
	Aliases: []string{"ver"},
	Short:   "Print the version number of ocli",
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("ocli version %s\n", Version)
	},
}

func init() {
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate("ocli version {{.Version}}\n")
	rootCmd.AddCommand(versionCmd)
}

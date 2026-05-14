package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newImportMigrateCmd(stdout, stderr io.Writer) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:        "migrate",
		Short:      "Deprecated PackV1 migration shim",
		Long:       "Deprecated PackV1 migration shim. Use gc doctor for migration guidance.",
		Hidden:     true,
		Deprecated: "use `gc doctor` for migration guidance instead",
		Args:       cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if doImportMigrate(dryRun, stdout, stderr) != 0 {
				return errExit
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would change without writing")
	return cmd
}

func doImportMigrate(dryRun bool, stdout, stderr io.Writer) int {
	_ = dryRun
	fmt.Fprintln(stderr, "gc import migrate has been retired as a PackV1 migration path.")                                              //nolint:errcheck // best-effort stderr
	fmt.Fprintln(stderr, "Run `gc doctor` to inventory legacy PackV1 surfaces and current PackV2 requirements.")                        //nolint:errcheck // best-effort stderr
	fmt.Fprintln(stderr, "Run `gc doctor --fix` only for safe mechanical remediation; PackV1 layouts are no longer upgraded in place.") //nolint:errcheck // best-effort stderr
	return 1
}

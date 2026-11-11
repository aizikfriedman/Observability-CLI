package cmd

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

var healthCmd = &cobra.Command{
	Use:     "health",
	Aliases: []string{"hc", "status", "ping"},
	Short:   "Check connectivity and health of all Grafana DataSources (exits with code 1 on error)",
	Example: "  ocli health\n  ocli hc",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := getClient()
		if err != nil {
			return err
		}

		results, err := client.CheckDataSourcesHealth()
		if err != nil {
			return err
		}

		if len(results) == 0 {
			fmt.Println("No datasources found.")
			return nil
		}

		const (
			maxNameRunes = 24
			maxMsgRunes  = 52
		)

		nameW, uidW, typeW, statusW, latW, msgW := 4, 3, 4, 6, 7, 7
		type row struct {
			name    string
			uid     string
			dtype   string
			status  string
			latency string
			message string
			isOK    bool
		}

		hasError := false
		rows := make([]row, 0, len(results))
		for _, h := range results {
			if !h.IsOK {
				hasError = true
			}
			r := row{
				name:    truncateRunes(h.Name, maxNameRunes),
				uid:     h.UID,
				dtype:   h.Type,
				status:  h.Status,
				latency: h.Latency,
				message: truncateRunes(h.Message, maxMsgRunes),
				isOK:    h.IsOK,
			}

			if l := utf8.RuneCountInString(r.name); l > nameW {
				nameW = l
			}
			if l := utf8.RuneCountInString(r.uid); l > uidW {
				uidW = l
			}
			if l := utf8.RuneCountInString(r.dtype); l > typeW {
				typeW = l
			}
			if l := utf8.RuneCountInString(r.status); l > statusW {
				statusW = l
			}
			if l := utf8.RuneCountInString(r.latency); l > latW {
				latW = l
			}
			if l := utf8.RuneCountInString(r.message); l > msgW {
				msgW = l
			}
			rows = append(rows, r)
		}

		header := fmt.Sprintf("%s | %s | %s | %s | %s | %s",
			padRightRunes("NAME", nameW),
			padRightRunes("UID", uidW),
			padRightRunes("TYPE", typeW),
			padRightRunes("STATUS", statusW),
			padRightRunes("LATENCY", latW),
			"MESSAGE",
		)
		sep := fmt.Sprintf("%s-+-%s-+-%s-+-%s-+-%s-+-%s",
			strings.Repeat("-", nameW),
			strings.Repeat("-", uidW),
			strings.Repeat("-", typeW),
			strings.Repeat("-", statusW),
			strings.Repeat("-", latW),
			strings.Repeat("-", msgW),
		)

		fmt.Println(header)
		fmt.Println(sep)
		for _, r := range rows {
			paddedStatus := padRightRunes(r.status, statusW)
			var coloredStatus string
			if r.isOK {
				coloredStatus = "\033[38;2;115;191;105m" + paddedStatus + "\033[0m"
			} else {
				coloredStatus = "\033[38;2;242;73;92m" + paddedStatus + "\033[0m"
			}

			fmt.Printf("%s | %s | %s | %s | %s | %s\n",
				padRightRunes(r.name, nameW),
				padRightRunes(r.uid, uidW),
				padRightRunes(r.dtype, typeW),
				coloredStatus,
				padRightRunes(r.latency, latW),
				r.message,
			)
		}

		if hasError {
			return ErrSilentExit1
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(healthCmd)
}

package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

var pnlCmd = &cobra.Command{
	Use:     "pnl <DASH_UID> <PANEL_ID>",
	Aliases: []string{"panel", "p"},
	Short:   "Fetch panel value, list dashboard panels, or inspect panel config",
	Example: "  ocli pnl 123 4\n  ocli pnl ls 123\n  ocli pnl info 123 4",
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		dashUID := args[0]
		panelID, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid panel ID %q: must be an integer", args[1])
		}

		client, err := getClient()
		if err != nil {
			return err
		}

		res, err := client.GetPanelValue(dashUID, panelID)
		if err != nil {
			return err
		}

		if res.ColorANSI != "" {
			fmt.Printf("%s[ %s ] %s\033[0m\n", res.ColorANSI, res.Title, res.Formatted)
		} else {
			fmt.Printf("[ %s ] %s\n", res.Title, res.Formatted)
		}
		return nil
	},
}

func truncateRunes(s string, maxRunes int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:maxRunes-3])) + "..."
}

func padRightRunes(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

var pnlListCmd = &cobra.Command{
	Use:     "ls <DASH_UID>",
	Aliases: []string{"list", "l"},
	Short:   "List all panels in a dashboard",
	Example: "  ocli pnl ls 123",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dashUID := args[0]

		client, err := getClient()
		if err != nil {
			return err
		}

		panels, err := client.ListPanels(dashUID)
		if err != nil {
			return err
		}

		if len(panels) == 0 {
			fmt.Println("No panels found.")
			return nil
		}

		const (
			maxTitleRunes = 38
			maxDescRunes  = 52
		)

		idW, typeW, titleW, descW := 2, 4, 5, 11
		type row struct {
			id    string
			ptype string
			title string
			desc  string
		}

		rows := make([]row, 0, len(panels))
		for _, p := range panels {
			r := row{
				id:    strconv.Itoa(p.ID),
				ptype: p.Type,
				title: truncateRunes(p.Title, maxTitleRunes),
				desc:  truncateRunes(p.Description, maxDescRunes),
			}
			if l := utf8.RuneCountInString(r.id); l > idW {
				idW = l
			}
			if l := utf8.RuneCountInString(r.ptype); l > typeW {
				typeW = l
			}
			if l := utf8.RuneCountInString(r.title); l > titleW {
				titleW = l
			}
			if l := utf8.RuneCountInString(r.desc); l > descW {
				descW = l
			}
			rows = append(rows, r)
		}

		header := fmt.Sprintf("%s | %s | %s | %s",
			padRightRunes("ID", idW),
			padRightRunes("TYPE", typeW),
			padRightRunes("TITLE", titleW),
			"DESCRIPTION",
		)
		sep := fmt.Sprintf("%s-+-%s-+-%s-+-%s",
			strings.Repeat("-", idW),
			strings.Repeat("-", typeW),
			strings.Repeat("-", titleW),
			strings.Repeat("-", descW),
		)

		fmt.Println(header)
		fmt.Println(sep)
		for _, r := range rows {
			fmt.Printf("%s | %s | %s | %s\n",
				padRightRunes(r.id, idW),
				padRightRunes(r.ptype, typeW),
				padRightRunes(r.title, titleW),
				r.desc,
			)
		}
		return nil
	},
}

var pnlInfoCmd = &cobra.Command{
	Use:     "info <DASH_UID> <PANEL_ID>",
	Aliases: []string{"i", "inspect"},
	Short:   "Inspect panel queries, datasource, and transformations",
	Example: "  ocli pnl info 123 4",
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		dashUID := args[0]
		panelID, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid panel ID %q: must be an integer", args[1])
		}

		client, err := getClient()
		if err != nil {
			return err
		}

		info, err := client.GetPanelInfo(dashUID, panelID)
		if err != nil {
			return err
		}

		fmt.Printf("[ Panel Info: %s (ID: %d) ]\n", info.Title, info.ID)
		fmt.Printf("Dashboard:  %s\n", info.DashboardUID)
		fmt.Printf("Type:       %s\n", info.Type)
		fmt.Printf("DataSource: %s\n", info.DataSource)
		for _, q := range info.Queries {
			label := fmt.Sprintf("Query %s:", q.RefID)
			fmt.Printf("%-11s %s\n", label, q.Expr)
		}
		for _, tr := range info.Transforms {
			fmt.Printf("Transform:  %s\n", tr)
		}
		return nil
	},
}

func init() {
	pnlCmd.AddCommand(pnlListCmd)
	pnlCmd.AddCommand(pnlInfoCmd)
	rootCmd.AddCommand(pnlCmd)
}

package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func resolveDashboardUID(explicit string) (string, error) {
	if explicit != "" {
		saveLastDashboard(explicit)
		return explicit, nil
	}
	if def := strings.TrimSpace(viper.GetString("default_dashboard")); def != "" {
		return def, nil
	}
	if last := strings.TrimSpace(viper.GetString("last_dashboard")); last != "" {
		return last, nil
	}
	return "", fmt.Errorf("no dashboard specified. Pass <DASH_UID> (e.g. 'ocli pnl ls app-deep-dive') or run 'ocli d ls' to see available dashboards")
}

func saveLastDashboard(uid string) {
	if uid == "" {
		return
	}
	viper.Set("last_dashboard", uid)
	if cfgPath, err := configFilePath(); err == nil {
		_ = viper.WriteConfigAs(cfgPath)
	}
}

var pnlCmd = &cobra.Command{
	Use:     "pnl [<DASH_UID>] <PANEL_ID>",
	Aliases: []string{"panel", "p"},
	Short:   "Fetch panel value, list dashboard panels, or inspect panel config",
	Example: "  ocli pnl app-deep-dive 2\n  ocli pnl 2                  (uses last/default dashboard)\n  ocli pnl ls app-deep-dive\n  ocli pnl info app-deep-dive 2",
	Args:    cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		var (
			dashUID string
			panelID int
			err     error
		)

		if len(args) == 1 {
			// If the single argument is an integer, treat it as <PANEL_ID> on the active/default dashboard.
			// Otherwise treat it as <DASH_UID> and list its panels!
			if id, convErr := strconv.Atoi(args[0]); convErr == nil {
				panelID = id
				dashUID, err = resolveDashboardUID("")
				if err != nil {
					return err
				}
			} else {
				return runPanelList(args[0])
			}
		} else {
			dashUID, err = resolveDashboardUID(args[0])
			if err != nil {
				return err
			}
			panelID, err = strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("invalid panel ID %q: must be an integer", args[1])
			}
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
	Use:     "ls [<DASH_UID>]",
	Aliases: []string{"list", "l"},
	Short:   "List all panels in a dashboard",
	Example: "  ocli pnl ls app-deep-dive\n  ocli pnl ls",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var explicit string
		if len(args) == 1 {
			explicit = args[0]
		}
		return runPanelList(explicit)
	},
}

func runPanelList(explicitUID string) error {
	dashUID, err := resolveDashboardUID(explicitUID)
	if err != nil {
		return err
	}

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
}

var pnlInfoCmd = &cobra.Command{
	Use:     "info [<DASH_UID>] <PANEL_ID>",
	Aliases: []string{"i", "inspect"},
	Short:   "Inspect panel queries, datasource, and transformations",
	Example: "  ocli pnl info app-deep-dive 2\n  ocli pnl info 2",
	Args:    cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		var (
			dashUID string
			panelID int
			err     error
		)
		if len(args) == 1 {
			dashUID, err = resolveDashboardUID("")
			if err != nil {
				return err
			}
			panelID, err = strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid panel ID %q: must be an integer", args[0])
			}
		} else {
			dashUID, err = resolveDashboardUID(args[0])
			if err != nil {
				return err
			}
			panelID, err = strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("invalid panel ID %q: must be an integer", args[1])
			}
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

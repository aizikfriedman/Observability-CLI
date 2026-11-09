package cmd

import (
	"fmt"
	"strconv"
	"strings"

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

		idW, typeW, titleW := 3, 10, 21
		for _, p := range panels {
			if l := len(strconv.Itoa(p.ID)); l > idW {
				idW = l
			}
			if l := len(p.Type); l > typeW {
				typeW = l
			}
			if l := len(p.Title); l > titleW {
				titleW = l
			}
		}

		header := fmt.Sprintf("%-*s | %-*s | %-*s | %s", idW, "ID", typeW, "TYPE", titleW, "TITLE", "DESCRIPTION")
		sepLen := len(header) + 12
		if sepLen < 65 {
			sepLen = 65
		}

		fmt.Println(header)
		fmt.Println(strings.Repeat("-", sepLen))
		for _, p := range panels {
			fmt.Printf("%-*d | %-*s | %-*s | %s\n", idW, p.ID, typeW, p.Type, titleW, p.Title, p.Description)
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

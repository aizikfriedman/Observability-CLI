package cmd

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

var dashboardCmd = &cobra.Command{
	Use:     "dashboard",
	Aliases: []string{"dash", "d"},
	Short:   "Manage and list Grafana dashboards",
	Example: "  ocli dashboard ls\n  ocli dash ls\n  ocli d ls",
	RunE:    runDashboardList,
}

var dashboardListCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list", "l"},
	Short:   "List all Grafana dashboards",
	Example: "  ocli dashboard ls\n  ocli dash ls\n  ocli d ls",
	Args:    cobra.NoArgs,
	RunE:    runDashboardList,
}

func runDashboardList(cmd *cobra.Command, args []string) error {
	client, err := getClient()
	if err != nil {
		return err
	}

	dashboards, err := client.ListDashboards()
	if err != nil {
		return err
	}

	if len(dashboards) == 0 {
		fmt.Println("No dashboards found.")
		return nil
	}

	const (
		maxTitleRunes  = 38
		maxFolderRunes = 24
		maxTagsRunes   = 40
	)

	uidW, titleW, folderW, tagsW := 3, 5, 6, 4
	type row struct {
		uid    string
		title  string
		folder string
		tags   string
	}

	rows := make([]row, 0, len(dashboards))
	for _, d := range dashboards {
		tagsStr := "[]"
		if len(d.Tags) > 0 {
			tagsStr = "[" + strings.Join(d.Tags, ", ") + "]"
		}

		r := row{
			uid:    d.UID,
			title:  truncateRunes(d.Title, maxTitleRunes),
			folder: truncateRunes(d.Folder, maxFolderRunes),
			tags:   truncateRunes(tagsStr, maxTagsRunes),
		}

		if l := utf8.RuneCountInString(r.uid); l > uidW {
			uidW = l
		}
		if l := utf8.RuneCountInString(r.title); l > titleW {
			titleW = l
		}
		if l := utf8.RuneCountInString(r.folder); l > folderW {
			folderW = l
		}
		if l := utf8.RuneCountInString(r.tags); l > tagsW {
			tagsW = l
		}
		rows = append(rows, r)
	}

	header := fmt.Sprintf("%s | %s | %s | %s",
		padRightRunes("UID", uidW),
		padRightRunes("TITLE", titleW),
		padRightRunes("FOLDER", folderW),
		"TAGS",
	)
	sep := fmt.Sprintf("%s-+-%s-+-%s-+-%s",
		strings.Repeat("-", uidW),
		strings.Repeat("-", titleW),
		strings.Repeat("-", folderW),
		strings.Repeat("-", tagsW),
	)

	fmt.Println(header)
	fmt.Println(sep)
	for _, r := range rows {
		fmt.Printf("%s | %s | %s | %s\n",
			padRightRunes(r.uid, uidW),
			padRightRunes(r.title, titleW),
			padRightRunes(r.folder, folderW),
			r.tags,
		)
	}
	return nil
}

func init() {
	dashboardCmd.AddCommand(dashboardListCmd)
	rootCmd.AddCommand(dashboardCmd)
}

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var alertsCmd = &cobra.Command{
	Use:     "alerts",
	Aliases: []string{"a"},
	Short:   "List all currently firing alerts",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := getClient()
		if err != nil {
			return err
		}

		alerts, err := client.GetFiringAlerts()
		if err != nil {
			return err
		}

		if len(alerts) == 0 {
			fmt.Println("No firing alerts.")
			return nil
		}

		for _, a := range alerts {
			if a.Summary != "" {
				fmt.Printf("[FIRING] %s - %s\n", a.Name, a.Summary)
			} else {
				fmt.Printf("[FIRING] %s\n", a.Name)
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(alertsCmd)
}

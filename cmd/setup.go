package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var setupCmd = &cobra.Command{
	Use:     "setup",
	Aliases: []string{"s"},
	Short:   "Configure Grafana URL, API Token, and DataSource aliases",
	RunE: func(cmd *cobra.Command, args []string) error {
		reader := bufio.NewReader(os.Stdin)

		url := os.Getenv("OCLI_URL")
		if url == "" {
			fmt.Print("Grafana URL: ")
			input, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			url = strings.TrimSpace(input)
		}

		token := os.Getenv("OCLI_TOKEN")
		if token == "" {
			fmt.Print("Grafana API Token: ")
			input, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			token = strings.TrimSpace(input)
		}

		if url == "" || token == "" {
			return fmt.Errorf("both Grafana URL and API Token are required")
		}

		viper.Set("url", url)
		viper.Set("token", token)

		// Preserve existing datasources map or initialize sensible defaults for -t / -v
		if !viper.IsSet("datasources") {
			viper.Set("datasources", map[string]string{
				"default": "",
				"t":       "Thanos-Prod-DS",
				"v":       "Victoria-Metrics-DS",
			})
		}

		cfgPath, err := configFilePath()
		if err != nil {
			return err
		}

		if err := viper.WriteConfigAs(cfgPath); err != nil {
			return err
		}
		_ = os.Chmod(cfgPath, 0600)

		fmt.Println("Configuration saved to", cfgPath)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}

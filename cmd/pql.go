package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	flagThanos bool
	flagVM     bool
	flagDS     string
)

var pqlCmd = &cobra.Command{
	Use:     "promql <QUERY>",
	Aliases: []string{"pql", "q", "query"},
	Short:   "Execute an instant PromQL or LogQL query via Grafana DataSource proxy",
	Example: "  ocli promql \"sum(servers) - 4\"\n  ocli pql -t \"up == 0\"\n  ocli pql -v \"rate(http_requests_total[5m])\"\n  ocli promql --ds dev \"node_load1\"",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := args[0]
		dsIdentifier := resolveTargetDataSource()

		client, err := getClient()
		if err != nil {
			return err
		}

		res, err := client.QueryPromQL(dsIdentifier, query)
		if err != nil {
			return err
		}

		if res.IsScalar {
			fmt.Println(res.ScalarVal)
			return nil
		}

		if len(res.VectorRows) == 0 {
			fmt.Println("No data.")
			return nil
		}

		valW := 10
		for _, r := range res.VectorRows {
			if len(r.Value) > valW {
				valW = len(r.Value)
			}
		}

		header := fmt.Sprintf("%-*s | %s", valW, "VALUE", "LABELS")
		sepLen := len(header) + 10
		if sepLen < 29 {
			sepLen = 29
		}

		fmt.Println(header)
		fmt.Println(strings.Repeat("-", sepLen))
		for _, r := range res.VectorRows {
			fmt.Printf("%-*s | %s\n", valW, r.Value, r.Labels)
		}
		return nil
	},
}

func resolveTargetDataSource() string {
	if flagDS != "" {
		if mapped := viper.GetString("datasources." + flagDS); mapped != "" {
			return mapped
		}
		return flagDS
	}
	if flagThanos {
		if mapped := viper.GetString("datasources.t"); mapped != "" {
			return mapped
		}
		if mapped := viper.GetString("datasources.thanos"); mapped != "" {
			return mapped
		}
		return "Thanos"
	}
	if flagVM {
		if mapped := viper.GetString("datasources.v"); mapped != "" {
			return mapped
		}
		if mapped := viper.GetString("datasources.vm"); mapped != "" {
			return mapped
		}
		return "VictoriaMetrics"
	}
	return viper.GetString("datasources.default")
}

func init() {
	pqlCmd.Flags().BoolVarP(&flagThanos, "thanos", "t", false, "Route query to Thanos DataSource (datasources.t)")
	pqlCmd.Flags().BoolVarP(&flagVM, "vm", "v", false, "Route query to VictoriaMetrics DataSource (datasources.v)")
	pqlCmd.Flags().StringVarP(&flagDS, "ds", "d", "", "DataSource alias from ~/.ocli.yaml or Grafana DataSource name/UID")
	rootCmd.AddCommand(pqlCmd)
}

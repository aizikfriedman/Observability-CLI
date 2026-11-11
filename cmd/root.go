package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ocli/internal/grafana"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// ErrSilentExit1 indicates that the command has already printed its output
// (such as firing alerts or unhealthy datasources) and the CLI should exit with code 1.
var ErrSilentExit1 = errors.New("exit status 1")

var rootCmd = &cobra.Command{
	Use:           "ocli",
	Short:         "Minimalist Grafana Observability CLI",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root CLI command.
func Execute() {
	if maybeWindowsDoubleClickInstall(os.Args, 5*time.Second) {
		return
	}
	rootCmd.Version = Version
	if err := rootCmd.Execute(); err != nil {
		if !errors.Is(err, ErrSilentExit1) {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		os.Exit(1)
	}
}

func init() {
	// Disable Cobra's default "This is a command line tool" Windows Explorer trap
	// so double-clicking the executable on Windows installs ocli into PATH and shows the welcome message.
	cobra.MousetrapHelpText = ""
	cobra.OnInitialize(initConfig)
}

func initConfig() {
	home, err := os.UserHomeDir()
	if err == nil {
		viper.AddConfigPath(home)
		viper.SetConfigName(".ocli")
		viper.SetConfigType("yaml")
	}

	viper.SetEnvPrefix("OCLI")
	viper.AutomaticEnv()

	_ = viper.ReadInConfig()
}

func getClient() (*grafana.Client, error) {
	url := viper.GetString("url")
	token := viper.GetString("token")

	if url == "" || token == "" {
		return nil, fmt.Errorf("missing configuration. Run 'ocli setup' or set OCLI_URL and OCLI_TOKEN")
	}

	return grafana.NewClient(url, token), nil
}

func configFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ocli.yaml"), nil
}

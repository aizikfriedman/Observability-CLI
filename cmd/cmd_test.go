package cmd

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/spf13/viper"
)

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	runErr := fn()
	_ = w.Close()
	os.Stdout = origStdout
	<-done
	_ = r.Close()

	return buf.String(), runErr
}

func resetPQLFlags() {
	flagThanos = false
	flagVM = false
	flagDS = ""
	if f := pqlCmd.Flags().Lookup("thanos"); f != nil {
		_ = f.Value.Set("false")
		f.Changed = false
	}
	if f := pqlCmd.Flags().Lookup("vm"); f != nil {
		_ = f.Value.Set("false")
		f.Changed = false
	}
	if f := pqlCmd.Flags().Lookup("ds"); f != nil {
		_ = f.Value.Set("")
		f.Changed = false
	}
}

func setupTestEnv(t *testing.T, srvURL string) string {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	viper.Reset()
	resetPQLFlags()

	if srvURL != "" {
		viper.Set("url", srvURL)
		viper.Set("token", "test-token")
		viper.Set("datasources.default", "prom-uid")
		viper.Set("datasources.t", "thanos-uid")
		viper.Set("datasources.v", "vm-uid")
		viper.Set("datasources.custom", "prom-uid")
	}
	return tmpHome
}

func TestTruncateAndPadRunes(t *testing.T) {
	raw := "Line one\n   line two   with extra spaces"
	if got := truncateRunes(raw, 100); got != "Line one line two with extra spaces" {
		t.Errorf("unexpected normalized string: %q", got)
	}

	if got := truncateRunes("abcdefghij", 7); got != "abcd..." {
		t.Errorf("truncateRunes ASCII = %q, want abcd...", got)
	}

	// Multi-byte Hebrew UTF-8 string
	heb := "בדיקת חיתוך טקסט בעברית"
	truncHeb := truncateRunes(heb, 10)
	if !utf8.ValidString(truncHeb) {
		t.Errorf("truncated Hebrew string is not valid UTF-8: %q", truncHeb)
	}
	if utf8.RuneCountInString(truncHeb) > 10 {
		t.Errorf("expected <= 10 runes, got %d (%q)", utf8.RuneCountInString(truncHeb), truncHeb)
	}

	padded := padRightRunes("שלום", 8)
	if utf8.RuneCountInString(padded) != 8 {
		t.Errorf("expected 8 runes after padding, got %d (%q)", utf8.RuneCountInString(padded), padded)
	}
	if got := padRightRunes("already-long", 5); got != "already-long" {
		t.Errorf("expected string longer than width to be unchanged, got %q", got)
	}
}

func TestResolveTargetDataSource(t *testing.T) {
	setupTestEnv(t, "http://localhost:3000")

	// Default
	if got := resolveTargetDataSource(); got != "prom-uid" {
		t.Errorf("default ds = %q, want prom-uid", got)
	}

	// -t flag with datasources.t
	flagThanos = true
	if got := resolveTargetDataSource(); got != "thanos-uid" {
		t.Errorf("-t ds = %q, want thanos-uid", got)
	}
	// -t fallback to datasources.thanos and then "Thanos"
	viper.Set("datasources.t", "")
	viper.Set("datasources.thanos", "thanos-alt")
	if got := resolveTargetDataSource(); got != "thanos-alt" {
		t.Errorf("-t alt ds = %q, want thanos-alt", got)
	}
	viper.Set("datasources.thanos", "")
	if got := resolveTargetDataSource(); got != "Thanos" {
		t.Errorf("-t fallback ds = %q, want Thanos", got)
	}
	flagThanos = false

	// -v flag with datasources.v
	flagVM = true
	if got := resolveTargetDataSource(); got != "vm-uid" {
		t.Errorf("-v ds = %q, want vm-uid", got)
	}
	// -v fallback to datasources.vm and then "VictoriaMetrics"
	viper.Set("datasources.v", "")
	viper.Set("datasources.vm", "vm-alt")
	if got := resolveTargetDataSource(); got != "vm-alt" {
		t.Errorf("-v alt ds = %q, want vm-alt", got)
	}
	viper.Set("datasources.vm", "")
	if got := resolveTargetDataSource(); got != "VictoriaMetrics" {
		t.Errorf("-v fallback ds = %q, want VictoriaMetrics", got)
	}
	flagVM = false

	// -d mapped alias
	flagDS = "custom"
	if got := resolveTargetDataSource(); got != "prom-uid" {
		t.Errorf("-d custom = %q, want prom-uid", got)
	}

	// -d direct unmapped UID
	flagDS = "direct-uid"
	if got := resolveTargetDataSource(); got != "direct-uid" {
		t.Errorf("-d direct-uid = %q, want direct-uid", got)
	}
	flagDS = ""
}

func TestResolveDashboardUID(t *testing.T) {
	setupTestEnv(t, "http://localhost:3000")

	if _, err := resolveDashboardUID(""); err == nil {
		t.Fatal("expected error when no dashboard UID is provided or saved")
	}

	uid, err := resolveDashboardUID("dash-1")
	if err != nil || uid != "dash-1" {
		t.Fatalf("resolveDashboardUID explicit failed: uid=%q, err=%v", uid, err)
	}

	uid, err = resolveDashboardUID("")
	if err != nil || uid != "dash-1" {
		t.Fatalf("expected saved last_dashboard dash-1, got %q (err=%v)", uid, err)
	}

	viper.Set("default_dashboard", "default-dash")
	uid, err = resolveDashboardUID("")
	if err != nil || uid != "default-dash" {
		t.Fatalf("expected default-dash, got %q (err=%v)", uid, err)
	}
}

func newCLIMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/datasources", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
			{"id":1,"uid":"prom-uid","name":"Prometheus-Very-Long-DataSource-Name-For-Truncation","type":"prometheus","isDefault":true},
			{"id":2,"uid":"thanos-uid","name":"Thanos","type":"prometheus","isDefault":false},
			{"id":3,"uid":"vm-uid","name":"VictoriaMetrics","type":"prometheus","isDefault":false}
		]`))
	})

	mux.HandleFunc("/api/datasources/uid/prom-uid", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":1,"uid":"prom-uid","name":"Prometheus","type":"prometheus","isDefault":true}`))
	})
	mux.HandleFunc("/api/datasources/uid/thanos-uid", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":2,"uid":"thanos-uid","name":"Thanos","type":"prometheus","isDefault":false}`))
	})
	mux.HandleFunc("/api/datasources/uid/vm-uid", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":3,"uid":"vm-uid","name":"VictoriaMetrics","type":"prometheus","isDefault":false}`))
	})

	mux.HandleFunc("/api/datasources/uid/prom-uid/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"OK","message":"Successfully queried the Prometheus API with a detailed health check message."}`))
	})
	mux.HandleFunc("/api/datasources/uid/thanos-uid/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"OK","message":"Thanos healthy"}`))
	})
	mux.HandleFunc("/api/datasources/uid/vm-uid/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"ERROR","message":"Connection refused"}`))
	})

	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
			{"uid":"app-deep-dive","title":"App Deep Dive","folderTitle":"Production","tags":["otel","tracing"]},
			{"uid":"empty-dash","title":"Empty Dashboard","folderTitle":"","tags":[]}
		]`))
	})

	mux.HandleFunc("/api/dashboards/uid/app-deep-dive", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"dashboard": {
				"panels": [
					{
						"id": 2,
						"title": "CPU Usage",
						"type": "timeseries",
						"description": "Node CPU utilization",
						"datasource": {"uid": "prom-uid"},
						"targets": [{"refId": "A", "expr": "cpu_usage"}],
						"fieldConfig": {"defaults": {"unit": "percent"}},
						"transformations": [{"id": "organize", "options": {"excludeByName": {"Time": true}}}]
					},
					{
						"id": 5,
						"title": "Static Note",
						"type": "text"
					}
				]
			}
		}`))
	})

	mux.HandleFunc("/api/dashboards/uid/empty-dash", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"dashboard":{"panels":[]}}`))
	})

	mux.HandleFunc("/api/datasources/proxy/uid/prom-uid/api/v1/query", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		if q == "empty_metric" {
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"env":"prod"},"value":[1700000000,"42.5"]}]}}`))
	})

	mux.HandleFunc("/api/datasources/proxy/uid/thanos-uid/api/v1/query", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"scalar","result":[1700000000,"108"]}}`))
	})

	mux.HandleFunc("/api/datasources/proxy/uid/vm-uid/api/v1/query", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"scalar","result":[1700000000,"256"]}}`))
	})

	mux.HandleFunc("/api/prometheus/grafana/api/v1/alerts", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"data": {
				"alerts": [
					{"state":"firing","labels":{"alertname":"HighCPU"},"annotations":{"summary":"CPU > 90%"}},
					{"state":"firing","labels":{"alertname":"NoSummaryAlert"},"annotations":{}}
				]
			}
		}`))
	})

	return httptest.NewServer(mux)
}

func TestCLICommandsEndToEnd(t *testing.T) {
	srv := newCLIMockServer(t)
	defer srv.Close()

	t.Run("SetupCommand_EnvAndStdin", func(t *testing.T) {
		tmpHome := setupTestEnv(t, "")
		t.Setenv("OCLI_URL", srv.URL)
		t.Setenv("OCLI_TOKEN", "setup-secret-token")

		out, err := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"setup"})
			return rootCmd.Execute()
		})
		if err != nil {
			t.Fatalf("setup command failed: %v", err)
		}
		if !strings.Contains(out, "Configuration saved to") {
			t.Errorf("unexpected setup output: %s", out)
		}
		cfgFile := filepath.Join(tmpHome, ".ocli.yaml")
		if _, statErr := os.Stat(cfgFile); statErr != nil {
			t.Errorf("expected config file at %s: %v", cfgFile, statErr)
		}

		// Test interactive Stdin input
		setupTestEnv(t, "")
		t.Setenv("OCLI_URL", "")
		t.Setenv("OCLI_TOKEN", "")
		origStdin := os.Stdin
		r, w, _ := os.Pipe()
		_, _ = w.WriteString(srv.URL + "\nstdin-token\n")
		_ = w.Close()
		os.Stdin = r
		defer func() { os.Stdin = origStdin }()

		outStdin, errStdin := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"s"})
			return rootCmd.Execute()
		})
		if errStdin != nil {
			t.Fatalf("setup via stdin failed: %v", errStdin)
		}
		if !strings.Contains(outStdin, "Configuration saved to") {
			t.Errorf("unexpected stdin setup output: %s", outStdin)
		}
	})

	t.Run("MissingConfigError", func(t *testing.T) {
		setupTestEnv(t, "")
		t.Setenv("OCLI_URL", "")
		t.Setenv("OCLI_TOKEN", "")

		rootCmd.SetArgs([]string{"alerts"})
		if err := rootCmd.Execute(); err == nil || !strings.Contains(err.Error(), "missing configuration") {
			t.Errorf("expected missing configuration error, got %v", err)
		}
	})

	t.Run("HealthCommand_Exits1OnError", func(t *testing.T) {
		setupTestEnv(t, srv.URL)
		out, err := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"health"})
			return rootCmd.Execute()
		})
		if !errors.Is(err, ErrSilentExit1) {
			t.Fatalf("expected ErrSilentExit1 when a datasource is in ERROR state, got: %v", err)
		}
		for _, want := range []string{"NAME", "UID", "STATUS", "Thanos", "VictoriaMetrics", "Connection refused"} {
			if !strings.Contains(out, want) {
				t.Errorf("health output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("HealthCommand_Exits0WhenAllHealthy", func(t *testing.T) {
		healthySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/datasources":
				_, _ = w.Write([]byte(`[{"id":1,"uid":"prom-uid","name":"Prometheus","type":"prometheus","isDefault":true}]`))
			case "/api/datasources/uid/prom-uid/health":
				_, _ = w.Write([]byte(`{"status":"OK","message":"Data source is working"}`))
			}
		}))
		defer healthySrv.Close()

		setupTestEnv(t, healthySrv.URL)
		out, err := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"health"})
			return rootCmd.Execute()
		})
		if err != nil {
			t.Fatalf("expected nil error when all datasources are healthy, got: %v", err)
		}
		if !strings.Contains(out, "Prometheus") || !strings.Contains(out, "OK") {
			t.Errorf("unexpected healthy output:\n%s", out)
		}
	})

	t.Run("DashboardListCommand", func(t *testing.T) {
		setupTestEnv(t, srv.URL)
		out, err := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"d", "ls"})
			return rootCmd.Execute()
		})
		if err != nil {
			t.Fatalf("dashboard ls failed: %v", err)
		}
		for _, want := range []string{"app-deep-dive", "App Deep Dive", "Production", "[otel, tracing]", "empty-dash", "General"} {
			if !strings.Contains(out, want) {
				t.Errorf("dashboard ls output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("PanelCommands_List_Value_Info", func(t *testing.T) {
		setupTestEnv(t, srv.URL)

		// 1. List panels via single non-integer argument (`ocli pnl app-deep-dive`)
		outLS, err := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"pnl", "app-deep-dive"})
			return rootCmd.Execute()
		})
		if err != nil {
			t.Fatalf("pnl app-deep-dive failed: %v", err)
		}
		for _, want := range []string{"ID", "TYPE", "TITLE", "CPU Usage", "Node CPU utilization"} {
			if !strings.Contains(outLS, want) {
				t.Errorf("pnl ls output missing %q:\n%s", want, outLS)
			}
		}

		// 2. Empty dashboard panels list (`ocli pnl ls empty-dash`)
		outEmptyLS, err := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"pnl", "ls", "empty-dash"})
			return rootCmd.Execute()
		})
		if err != nil || !strings.Contains(outEmptyLS, "No panels found.") {
			t.Errorf("expected 'No panels found.', got %q (err=%v)", outEmptyLS, err)
		}

		// 3. Fetch panel value with explicit dashboard (`ocli pnl app-deep-dive 2`)
		outVal, err := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"pnl", "app-deep-dive", "2"})
			return rootCmd.Execute()
		})
		if err != nil {
			t.Fatalf("pnl app-deep-dive 2 failed: %v", err)
		}
		if !strings.Contains(outVal, "[ CPU Usage ] 42.5%") {
			t.Errorf("unexpected pnl value output: %q", outVal)
		}

		// 4. Fetch non-colored panel value using remembered last_dashboard (`ocli pnl 5`)
		outVal5, err := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"pnl", "5"})
			return rootCmd.Execute()
		})
		if err != nil || !strings.Contains(outVal5, "[ Static Note ] N/A") {
			t.Errorf("unexpected pnl 5 output: %q (err=%v)", outVal5, err)
		}

		// 5. Inspect panel info (`ocli pnl info 2` using remembered dashboard)
		outInfo, err := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"pnl", "info", "2"})
			return rootCmd.Execute()
		})
		if err != nil {
			t.Fatalf("pnl info 2 failed: %v", err)
		}
		for _, want := range []string{"[ Panel Info: CPU Usage (ID: 2) ]", "DataSource: prom-uid", "Query A:    cpu_usage", "Organize fields (Hidden: Time)"} {
			if !strings.Contains(outInfo, want) {
				t.Errorf("pnl info output missing %q:\n%s", want, outInfo)
			}
		}

		// 6. Invalid panel ID errors
		rootCmd.SetArgs([]string{"pnl", "app-deep-dive", "not-an-int"})
		if err := rootCmd.Execute(); err == nil || !strings.Contains(err.Error(), "must be an integer") {
			t.Errorf("expected integer error for panel ID, got %v", err)
		}
		rootCmd.SetArgs([]string{"pnl", "info", "not-an-int"})
		if err := rootCmd.Execute(); err == nil || !strings.Contains(err.Error(), "must be an integer") {
			t.Errorf("expected integer error for pnl info 1-arg, got %v", err)
		}
		rootCmd.SetArgs([]string{"pnl", "info", "app-deep-dive", "not-an-int"})
		if err := rootCmd.Execute(); err == nil || !strings.Contains(err.Error(), "must be an integer") {
			t.Errorf("expected integer error for pnl info 2-arg, got %v", err)
		}
	})

	t.Run("PromQLCommand", func(t *testing.T) {
		setupTestEnv(t, srv.URL)

		// Vector query
		resetPQLFlags()
		outVec, err := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"pql", "cpu_usage"})
			return rootCmd.Execute()
		})
		if err != nil {
			t.Fatalf("pql vector failed: %v", err)
		}
		if !strings.Contains(outVec, "42.5") || !strings.Contains(outVec, `{env="prod"}`) {
			t.Errorf("unexpected pql vector output:\n%s", outVec)
		}

		// Scalar query via -t (Thanos)
		resetPQLFlags()
		outThanos, err := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"pql", "-t", "scalar(up)"})
			return rootCmd.Execute()
		})
		if err != nil || strings.TrimSpace(outThanos) != "108" {
			t.Errorf("expected Thanos scalar 108, got %q (err=%v)", outThanos, err)
		}

		// Scalar query via -v (VictoriaMetrics)
		resetPQLFlags()
		outVM, err := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"pql", "-v", "scalar(up)"})
			return rootCmd.Execute()
		})
		if err != nil || strings.TrimSpace(outVM) != "256" {
			t.Errorf("expected VM scalar 256, got %q (err=%v)", outVM, err)
		}

		// Empty vector query
		resetPQLFlags()
		outEmpty, err := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"pql", "empty_metric"})
			return rootCmd.Execute()
		})
		if err != nil {
			t.Fatalf("pql empty failed: %v", err)
		}
		if !strings.Contains(outEmpty, "No data.") {
			t.Errorf("expected 'No data.', got %q", outEmpty)
		}
	})

	t.Run("AlertsCommand_Exits1WhenFiring", func(t *testing.T) {
		setupTestEnv(t, srv.URL)
		out, err := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"alerts"})
			return rootCmd.Execute()
		})
		if !errors.Is(err, ErrSilentExit1) {
			t.Fatalf("expected ErrSilentExit1 when alerts are firing, got: %v", err)
		}
		if !strings.Contains(out, "[FIRING] HighCPU - CPU > 90%") || !strings.Contains(out, "[FIRING] NoSummaryAlert") {
			t.Errorf("unexpected alerts output:\n%s", out)
		}
	})

	t.Run("VersionAndNoArgsHelp", func(t *testing.T) {
		setupTestEnv(t, srv.URL)
		Version = "v1.2.3"
		rootCmd.Version = Version

		outVerCmd, err := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"version"})
			return rootCmd.Execute()
		})
		if err != nil || strings.TrimSpace(outVerCmd) != "ocli version v1.2.3" {
			t.Errorf("unexpected version command output: %q (err=%v)", outVerCmd, err)
		}

		var flagBuf bytes.Buffer
		rootCmd.SetOut(&flagBuf)
		rootCmd.SetArgs([]string{"--version"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("--version flag failed: %v", err)
		}
		if vf := rootCmd.Flags().Lookup("version"); vf != nil {
			_ = vf.Value.Set("false")
			vf.Changed = false
		}
		rootCmd.SetOut(nil)
		if strings.TrimSpace(flagBuf.String()) != "ocli version v1.2.3" {
			t.Errorf("unexpected --version output: %q", flagBuf.String())
		}

		// Running ocli with no arguments prints help and exits 0 (no auto-install)
		var helpBuf bytes.Buffer
		rootCmd.SetOut(&helpBuf)
		rootCmd.SetArgs([]string{})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("no-args execution failed: %v", err)
		}
		rootCmd.SetOut(nil)
		if !strings.Contains(helpBuf.String(), "Available Commands:") {
			t.Errorf("expected Help output on no-args run, got:\n%s", helpBuf.String())
		}
	})

	t.Run("EmptyResponses_Health_Dashboards_Alerts", func(t *testing.T) {
		emptySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/datasources", "/api/search":
				_, _ = w.Write([]byte(`[]`))
			case "/api/prometheus/grafana/api/v1/alerts":
				_, _ = w.Write([]byte(`{"data":{"alerts":[]}}`))
			}
		}))
		defer emptySrv.Close()

		setupTestEnv(t, emptySrv.URL)

		outHealth, err := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"hc"})
			return rootCmd.Execute()
		})
		if err != nil || !strings.Contains(outHealth, "No datasources found.") {
			t.Errorf("expected 'No datasources found.', got %q (err=%v)", outHealth, err)
		}

		outDash, err := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"d"})
			return rootCmd.Execute()
		})
		if err != nil || !strings.Contains(outDash, "No dashboards found.") {
			t.Errorf("expected 'No dashboards found.', got %q (err=%v)", outDash, err)
		}

		outAlerts, err := captureStdout(t, func() error {
			rootCmd.SetArgs([]string{"a"})
			return rootCmd.Execute()
		})
		if err != nil || !strings.Contains(outAlerts, "No firing alerts.") {
			t.Errorf("expected 'No firing alerts.', got %q (err=%v)", outAlerts, err)
		}
	})
}

func TestInstallHelpers(t *testing.T) {
	// Test containsPathDir, appendToPathList & removeFromPathList (case-insensitive Windows style)
	winPath := `C:\Windows\System32;C:\Users\User\AppData\Local\ocli\`
	if !containsPathDir(winPath, `c:\users\user\appdata\local\ocli`, true) {
		t.Error("expected containsPathDir to match case-insensitively with trailing slash")
	}
	if _, updated := appendToPathList(winPath, `C:\Users\User\AppData\Local\ocli`, true); updated {
		t.Error("expected appendToPathList not to duplicate existing directory")
	}
	newWinPath, updated := appendToPathList(winPath, `C:\Custom\Bin`, true)
	if !updated || !strings.HasSuffix(newWinPath, `;C:\Custom\Bin`) {
		t.Errorf("unexpected appended path: %q (updated=%v)", newWinPath, updated)
	}
	cleanedPath, removed := removeFromPathList(newWinPath, `c:\custom\bin\`, true)
	if !removed || containsPathDir(cleanedPath, `C:\Custom\Bin`, true) {
		t.Errorf("expected removeFromPathList to remove C:\\Custom\\Bin, got %q (removed=%v)", cleanedPath, removed)
	}

	// Test copyExecutableFile
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "src.bin")
	dstFile := filepath.Join(tmpDir, "nested", "dst.bin")
	if err := os.WriteFile(srcFile, []byte("binary-payload"), 0755); err != nil {
		t.Fatalf("failed to write src file: %v", err)
	}
	if err := copyExecutableFile(srcFile, dstFile); err != nil {
		t.Fatalf("copyExecutableFile failed: %v", err)
	}
	gotBytes, err := os.ReadFile(dstFile)
	if err != nil || string(gotBytes) != "binary-payload" {
		t.Errorf("unexpected copied file content: %q (err=%v)", string(gotBytes), err)
	}
	if err := copyExecutableFile(dstFile, dstFile); err != nil {
		t.Errorf("expected self-copy to succeed as no-op, got %v", err)
	}

	// Test Unix interactive [Y/n] prompt: declining ('n') must NOT touch .zshrc
	homeDir := t.TempDir()
	zshrc := filepath.Join(homeDir, ".zshrc")
	if err := os.WriteFile(zshrc, []byte("# existing zshrc\n"), 0644); err != nil {
		t.Fatalf("failed to create .zshrc: %v", err)
	}
	installDir := filepath.Join(homeDir, ".local", "bin")
	var promptOut bytes.Buffer
	if err := promptAndConfigureUnixPath(homeDir, installDir, "/usr/bin:/bin", strings.NewReader("n\n"), &promptOut); err != nil {
		t.Fatalf("promptAndConfigureUnixPath (n) failed: %v", err)
	}
	afterDecline, _ := os.ReadFile(zshrc)
	if strings.Contains(string(afterDecline), ".local/bin") {
		t.Error("expected .zshrc NOT to be modified when user answers 'n'")
	}
	if !strings.Contains(promptOut.String(), "Skipped modifying shell profile") {
		t.Errorf("expected skip message in output, got: %s", promptOut.String())
	}

	// Test Unix interactive [Y/n] prompt: accepting ('y') appends export line, and cleanUnixShellProfiles removes it
	promptOut.Reset()
	if err := promptAndConfigureUnixPath(homeDir, installDir, "/usr/bin:/bin", strings.NewReader("y\n"), &promptOut); err != nil {
		t.Fatalf("promptAndConfigureUnixPath (y) failed: %v", err)
	}
	afterAccept, _ := os.ReadFile(zshrc)
	if !strings.Contains(string(afterAccept), unixPathExportLine) {
		t.Errorf("expected .zshrc to contain %q after accepting, got: %s", unixPathExportLine, string(afterAccept))
	}

	cleanUnixShellProfiles(homeDir)
	afterClean, _ := os.ReadFile(zshrc)
	if strings.Contains(string(afterClean), unixPathExportLine) {
		t.Errorf("expected cleanUnixShellProfiles to remove export line, got: %s", string(afterClean))
	}

	// Test maybeWindowsDoubleClickInstall does not trigger when arguments are present
	if maybeWindowsDoubleClickInstall([]string{"ocli", "health"}, 0) {
		t.Error("expected maybeWindowsDoubleClickInstall to return false when subcommands are passed")
	}
}

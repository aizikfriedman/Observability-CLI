package grafana

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClient(t *testing.T) {
	c := NewClient("https://grafana.example.com///", "secret-token")
	if c.BaseURL != "https://grafana.example.com" {
		t.Fatalf("expected trimmed BaseURL, got %q", c.BaseURL)
	}
	if c.Token != "secret-token" {
		t.Fatalf("expected token %q, got %q", "secret-token", c.Token)
	}
	if c.httpClient == nil {
		t.Fatal("expected non-nil httpClient")
	}
}

func TestFlattenPanels(t *testing.T) {
	panels := []rawPanel{
		{ID: 1, Title: "Top Panel", Type: "stat"},
		{
			ID:    2,
			Title: "Row 1",
			Type:  "row",
			Panels: []rawPanel{
				{ID: 3, Title: "Nested in Row", Type: "timeseries"},
				{ID: 4, Title: "Nested Stat", Type: "stat"},
			},
		},
		{
			ID:    5,
			Title: "Group Panel",
			Type:  "group",
			Panels: []rawPanel{
				{ID: 6, Title: "Child of Group", Type: "gauge"},
			},
		},
	}

	flat := flattenPanels(panels)
	expectedIDs := []int{1, 3, 4, 5, 6}
	if len(flat) != len(expectedIDs) {
		t.Fatalf("expected %d flattened panels, got %d", len(expectedIDs), len(flat))
	}
	for i, want := range expectedIDs {
		if flat[i].ID != want {
			t.Errorf("flat[%d].ID = %d, want %d", i, flat[i].ID, want)
		}
	}
}

func TestParseDataSourceRef(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", ""},
		{"null", "null", ""},
		{"string", `"Prometheus-Prod"`, "Prometheus-Prod"},
		{"object with uid", `{"uid":"prom-1","name":"Prometheus","type":"prometheus"}`, "prom-1"},
		{"object with name only", `{"name":"Prometheus","type":"prometheus"}`, "Prometheus"},
		{"object with type only", `{"type":"loki"}`, "loki"},
		{"invalid json", `{invalid`, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseDataSourceRef(json.RawMessage(tc.raw))
			if got != tc.want {
				t.Errorf("parseDataSourceRef(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestBuildTemplateVarsMapAndInterpolate(t *testing.T) {
	rawVars := []rawTemplateVar{
		{
			Name: "cluster",
			Type: "query",
			Current: struct {
				Value json.RawMessage `json:"value"`
				Text  json.RawMessage `json:"text"`
			}{Value: json.RawMessage(`"prod-eu"`)},
		},
		{
			Name: "cluster_name",
			Type: "query",
			Current: struct {
				Value json.RawMessage `json:"value"`
				Text  json.RawMessage `json:"text"`
			}{Value: json.RawMessage(`"production-europe-full"`)},
		},
		{
			Name: "namespace",
			Type: "query",
			Current: struct {
				Value json.RawMessage `json:"value"`
				Text  json.RawMessage `json:"text"`
			}{Value: json.RawMessage(`["ns-a","ns-b"]`)},
		},
		{
			Name:       "pod",
			Type:       "query",
			IncludeAll: true,
			Current: struct {
				Value json.RawMessage `json:"value"`
				Text  json.RawMessage `json:"text"`
			}{Value: json.RawMessage(`["$__all"]`)},
		},
		{
			Name:     "service",
			Type:     "query",
			AllValue: ".+",
			Current: struct {
				Value json.RawMessage `json:"value"`
				Text  json.RawMessage `json:"text"`
			}{Value: json.RawMessage(`"$__all"`)},
		},
		{
			Name:  "DS_PROMETHEUS",
			Type:  "datasource",
			Query: json.RawMessage(`"prometheus"`),
			Current: struct {
				Value json.RawMessage `json:"value"`
				Text  json.RawMessage `json:"text"`
			}{Value: json.RawMessage(`"default"`)},
		},
		{
			Name: "", // Should be ignored
			Type: "custom",
		},
	}

	vars := buildTemplateVarsMap(rawVars)

	if vars["__rate_interval"] != "5m" || vars["__interval"] != "1m" {
		t.Fatalf("missing default built-in variables: %+v", vars)
	}
	if vars["cluster"] != "prod-eu" {
		t.Errorf("cluster = %q, want prod-eu", vars["cluster"])
	}
	if vars["cluster_name"] != "production-europe-full" {
		t.Errorf("cluster_name = %q, want production-europe-full", vars["cluster_name"])
	}
	if vars["namespace"] != "ns-a|ns-b" {
		t.Errorf("namespace = %q, want ns-a|ns-b", vars["namespace"])
	}
	if vars["pod"] != ".*" {
		t.Errorf("pod = %q, want .*", vars["pod"])
	}
	if vars["service"] != ".+" {
		t.Errorf("service = %q, want .+", vars["service"])
	}
	if vars["DS_PROMETHEUS"] != "prometheus" {
		t.Errorf("DS_PROMETHEUS = %q, want prometheus", vars["DS_PROMETHEUS"])
	}

	expr := `sum(rate(http_requests_total{cluster="$cluster", full="$cluster_name", ns=~"${namespace:regex}", pod=~"${pod:pipe}", svc="${service:raw}"}[$__rate_interval]))`
	got := interpolateExpr(expr, vars)
	want := `sum(rate(http_requests_total{cluster="prod-eu", full="production-europe-full", ns=~"ns-a|ns-b", pod=~".*", svc=".+"}[5m]))`
	if got != want {
		t.Errorf("interpolateExpr:\ngot:  %s\nwant: %s", got, want)
	}

	// Unchanged when no variables present
	plain := `up{job="node"}`
	if interpolateExpr(plain, vars) != plain {
		t.Errorf("expected plain expression to remain unchanged")
	}
}

func TestFormatWithUnitAndSmartNumber(t *testing.T) {
	tests := []struct {
		rawVal string
		unit   string
		want   string
	}{
		{"0", "", "0"},
		{"42", "", "42"},
		{"42.00", "", "42"},
		{"-17", "", "-17"},
		{"0.125", "", "0.125"},
		{"-0.375", "", "-0.375"},
		{"0.500", "", "0.5"},
		{"42.567", "", "42.57"},
		{"not-a-number", "percent", "not-a-number"},
		{"15.2", "reqps", "15.2 req/s"},
		{"100", "ops", "100 ops/s"},
		{"8.5", "rps", "8.5 r/s"},
		{"3.25", "wps", "3.25 W/s"},
		{"450", "iops", "450 iops"},
		{"0", "percent", "0%"},
		{"85.4", "percent", "85.4%"},
		{"0", "percentunit", "0%"},
		{"0.425", "percentunit", "42.5%"},
		{"1024", "bytes", "1024 B"},
		{"2048", "decbytes", "2048 B"},
		{"1.5", "s", "1.5s"},
		{"2", "sec", "2s"},
		{"38.4", "ms", "38.4ms"},
	}

	for _, tc := range tests {
		got := formatWithUnit(tc.rawVal, tc.unit)
		if got != tc.want {
			t.Errorf("formatWithUnit(%q, %q) = %q, want %q", tc.rawVal, tc.unit, got, tc.want)
		}
	}
}

func TestGrafanaColorToANSI(t *testing.T) {
	tests := []struct {
		color string
		want  string
	}{
		{"", ""},
		{"transparent", ""},
		{"text", ""},
		{"unknown-color", ""},
		{"green", "\033[38;2;115;191;105m"},
		{"  RED ", "\033[38;2;242;73;92m"},
		{"yellow", "\033[38;2;250;222;42m"},
		{"#ff0080", "\033[38;2;255;0;128m"},
		{"#f0a", "\033[38;2;255;0;170m"},
		{"#zzzzzz", ""},
		{"rgb(10, 20, 30)", "\033[38;2;10;20;30m"},
		{"rgba(100, 200, 50, 0.5)", "\033[38;2;100;200;50m"},
		{"rgb(bad)", ""},
	}

	for _, tc := range tests {
		got := grafanaColorToANSI(tc.color)
		if got != tc.want {
			t.Errorf("grafanaColorToANSI(%q) = %q, want %q", tc.color, got, tc.want)
		}
	}
}

func TestResolvePanelColorANSI(t *testing.T) {
	greenANSI := grafanaColorToANSI("green")
	yellowANSI := grafanaColorToANSI("yellow")
	redANSI := grafanaColorToANSI("red")
	blueANSI := grafanaColorToANSI("blue")

	// 1. Value mappings priority
	mappedPanel := rawPanel{}
	mappedPanel.FieldConfig.Defaults.Mappings = []struct {
		Type    string          `json:"type"`
		Options json.RawMessage `json:"options"`
	}{
		{
			Type:    "value",
			Options: json.RawMessage(`{"0":{"text":"DOWN","color":"red"},"1":{"text":"UP","color":"green"}}`),
		},
	}
	if got := resolvePanelColorANSI("0", mappedPanel); got != redANSI {
		t.Errorf("expected red ANSI from value mapping, got %q", got)
	}

	// 2. Fixed color priority
	fixedPanel := rawPanel{}
	fixedPanel.FieldConfig.Defaults.Color.Mode = "fixed"
	fixedPanel.FieldConfig.Defaults.Color.FixedColor = "blue"
	if got := resolvePanelColorANSI("42", fixedPanel); got != blueANSI {
		t.Errorf("expected blue ANSI from fixed color, got %q", got)
	}

	// 3. Modern thresholds steps
	v70 := 70.0
	v90 := 90.0
	threshPanel := rawPanel{}
	threshPanel.FieldConfig.Defaults.Thresholds.Steps = []struct {
		Color string   `json:"color"`
		Value *float64 `json:"value"`
	}{
		{Color: "green", Value: nil},
		{Color: "yellow", Value: &v70},
		{Color: "red", Value: &v90},
	}
	if got := resolvePanelColorANSI("50", threshPanel); got != greenANSI {
		t.Errorf("expected green for 50, got %q", got)
	}
	if got := resolvePanelColorANSI("75", threshPanel); got != yellowANSI {
		t.Errorf("expected yellow for 75, got %q", got)
	}
	if got := resolvePanelColorANSI("95", threshPanel); got != redANSI {
		t.Errorf("expected red for 95, got %q", got)
	}

	// 4. Legacy singlestat thresholds
	legacyPanel := rawPanel{
		Thresholds: "70, 90",
		Colors:     []string{"green", "yellow", "red"},
	}
	if got := resolvePanelColorANSI("80", legacyPanel); got != yellowANSI {
		t.Errorf("expected yellow for legacy 80, got %q", got)
	}
	if got := resolvePanelColorANSI("99", legacyPanel); got != redANSI {
		t.Errorf("expected red for legacy 99, got %q", got)
	}

	// 5. Fallback for numeric vs non-numeric
	emptyPanel := rawPanel{}
	if got := resolvePanelColorANSI("12.3", emptyPanel); got != greenANSI {
		t.Errorf("expected default green for numeric, got %q", got)
	}
	if got := resolvePanelColorANSI("N/A", emptyPanel); got != "" {
		t.Errorf("expected empty ANSI for non-numeric, got %q", got)
	}
}

func newMockGrafanaServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// Verify Bearer token middleware
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, `{"message":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	})

	// 1. Search dashboards
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("type") != "dash-db" {
			http.Error(w, "bad type", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`[
			{"uid":"app-deep-dive","title":"App Deep Dive","folderTitle":"Production","tags":["otel","tracing"]},
			{"uid":"root-dash","title":"Root Overview","folderTitle":"","tags":[]}
		]`))
	})

	// 2. Dashboard by UID
	mux.HandleFunc("/api/dashboards/uid/app-deep-dive", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"dashboard": {
				"templating": {
					"list": [
						{"name": "job", "type": "query", "current": {"value": "api-server"}},
						{"name": "DS_PROMETHEUS", "type": "datasource", "query": "prometheus", "current": {"value": "default"}}
					]
				},
				"panels": [
					{
						"id": 1,
						"title": "Overview Row",
						"type": "row",
						"panels": [
							{
								"id": 2,
								"title": "CPU Usage",
								"type": "timeseries",
								"description": "Node CPU\nutilization",
								"datasource": {"uid": "prom-uid"},
								"targets": [
									{"refId": "A", "expr": "avg(rate(cpu_seconds_total{job=\"$job\"}[$__rate_interval])) * 100"}
								],
								"fieldConfig": {
									"defaults": {
										"unit": "percent",
										"thresholds": {
											"mode": "absolute",
											"steps": [
												{"color": "green", "value": null},
												{"color": "red", "value": 80}
											]
										}
									}
								},
								"transformations": [
									{"id": "organize", "options": {"excludeByName": {"Time": true, "Value": false, " __name__": true}}},
									{"id": "merge"}
								]
							}
						]
					},
					{
						"id": 4,
						"title": "Empty Stat With NoValue",
						"type": "stat",
						"datasource": "$DS_PROMETHEUS",
						"targets": [
							{"expr": "absent_metric"}
						],
						"fieldConfig": {
							"defaults": {
								"noValue": "IDLE"
							}
						}
					},
					{
						"id": 5,
						"title": "Text Note",
						"type": "text"
					},
					{
						"id": 99,
						"title": "Empty Row",
						"type": "row"
					}
				]
			}
		}`))
	})

	// 3. Datasources endpoints
	mux.HandleFunc("/api/datasources/name/Thanos-Prod", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":2,"uid":"thanos-uid","name":"Thanos-Prod","type":"prometheus","isDefault":false}`))
	})
	mux.HandleFunc("/api/datasources/name/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/datasources/uid/prom-uid", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":1,"uid":"prom-uid","name":"Prometheus","type":"prometheus","isDefault":true}`))
	})
	mux.HandleFunc("/api/datasources/uid/", func(w http.ResponseWriter, r *http.Request) {
		// Check if it's a health check request: /api/datasources/uid/<uid>/health
		if strings.HasSuffix(r.URL.Path, "/health") {
			if strings.Contains(r.URL.Path, "prom-uid") {
				_, _ = w.Write([]byte(`{"status":"OK","message":"Successfully queried the Prometheus API."}`))
				return
			}
			if strings.Contains(r.URL.Path, "loki-uid") {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"status":"ERROR","message":"Loki unreachable"}`))
				return
			}
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/datasources/3/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`plain ok`))
	})
	mux.HandleFunc("/api/datasources", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
			{"id":1,"uid":"prom-uid","name":"Prometheus","type":"prometheus","isDefault":true},
			{"id":2,"uid":"loki-uid","name":"Loki","type":"loki","isDefault":false},
			{"id":3,"uid":"","name":"LegacyVM","type":"victoriametrics-datasource","isDefault":false}
		]`))
	})

	// 4. Datasource proxy for PromQL & Loki
	mux.HandleFunc("/api/datasources/proxy/uid/prom-uid/api/v1/query", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		switch {
		case strings.Contains(q, "cpu_seconds_total"):
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"job":"api-server","instance":"10.0.0.1:8080"},"value":[1700000000,"85.5"]}]}}`))
		case q == "scalar(up)":
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"scalar","result":[1700000000,"1"]}}`))
		case q == "absent_metric":
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
		case q == "bad_query":
			_, _ = w.Write([]byte(`{"status":"error","error":"parse error at char 1"}`))
		default:
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1700000000,"12"]}]}}`))
		}
	})

	mux.HandleFunc("/api/datasources/proxy/uid/thanos-uid/api/v1/query", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"scalar","result":[1700000000,"42"]}}`))
	})

	mux.HandleFunc("/api/datasources/proxy/3/api/v1/query", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"scalar","result":[1700000000,"99"]}}`))
	})

	// Loki instant query fails for log stream selector, triggering fallback to query_range
	mux.HandleFunc("/api/datasources/proxy/uid/loki-uid/loki/api/v1/query", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "instant query not supported for log streams", http.StatusBadRequest)
	})
	mux.HandleFunc("/api/datasources/proxy/uid/loki-uid/loki/api/v1/query_range", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[{"stream":{"app":"nginx","level":"error"},"values":[["1700000000000","connection refused"]]}]}}`))
	})

	// 5. Alerts endpoint
	mux.HandleFunc("/api/prometheus/grafana/api/v1/alerts", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"data": {
				"alerts": [
					{"state":"firing","labels":{"alertname":"HighMemory"},"annotations":{"summary":"Memory > 90%"}},
					{"state":"pending","labels":{"alertname":"DiskSpace"},"annotations":{"summary":"Disk > 80%"}},
					{"state":"FIRING","labels":{},"annotations":{"description":"Fallback description"}}
				]
			}
		}`))
	})

	return httptest.NewServer(handler)
}

func TestClientIntegrationWithMockServer(t *testing.T) {
	srv := newMockGrafanaServer(t)
	defer srv.Close()

	client := NewClient(srv.URL, "test-token")

	t.Run("ListDashboards", func(t *testing.T) {
		dashboards, err := client.ListDashboards()
		if err != nil {
			t.Fatalf("ListDashboards failed: %v", err)
		}
		if len(dashboards) != 2 {
			t.Fatalf("expected 2 dashboards, got %d", len(dashboards))
		}
		if dashboards[0].UID != "app-deep-dive" || dashboards[0].Folder != "Production" {
			t.Errorf("unexpected first dashboard: %+v", dashboards[0])
		}
		if dashboards[1].Folder != "General" {
			t.Errorf("expected empty folderTitle to default to General, got %q", dashboards[1].Folder)
		}
	})

	t.Run("ListPanels", func(t *testing.T) {
		panels, err := client.ListPanels("app-deep-dive")
		if err != nil {
			t.Fatalf("ListPanels failed: %v", err)
		}
		if len(panels) != 3 {
			t.Fatalf("expected 3 non-row panels, got %d", len(panels))
		}
		if panels[0].ID != 2 || panels[0].Description != "Node CPU utilization" {
			t.Errorf("unexpected panel[0]: %+v", panels[0])
		}
	})

	t.Run("GetPanelInfo", func(t *testing.T) {
		info, err := client.GetPanelInfo("app-deep-dive", 2)
		if err != nil {
			t.Fatalf("GetPanelInfo failed: %v", err)
		}
		if info.Title != "CPU Usage" || info.DataSource != "prom-uid" {
			t.Errorf("unexpected panel info: %+v", info)
		}
		if len(info.Queries) != 1 || info.Queries[0].RefID != "A" {
			t.Errorf("unexpected queries: %+v", info.Queries)
		}
		if len(info.Transforms) != 2 || !strings.Contains(info.Transforms[0], "Organize fields (Hidden:  __name__, Time)") {
			t.Errorf("unexpected transforms: %+v", info.Transforms)
		}

		if _, err := client.GetPanelInfo("app-deep-dive", 999); err == nil {
			t.Error("expected error for non-existent panel ID")
		}
	})

	t.Run("GetPanelValue", func(t *testing.T) {
		val, err := client.GetPanelValue("app-deep-dive", 2)
		if err != nil {
			t.Fatalf("GetPanelValue(2) failed: %v", err)
		}
		if val.Formatted != "85.5%" {
			t.Errorf("expected 85.5%%, got %q", val.Formatted)
		}
		if val.ColorANSI != grafanaColorToANSI("red") {
			t.Errorf("expected red threshold ANSI, got %q", val.ColorANSI)
		}

		// Panel 4 has empty vector result and custom NoValue "IDLE"
		val4, err := client.GetPanelValue("app-deep-dive", 4)
		if err != nil {
			t.Fatalf("GetPanelValue(4) failed: %v", err)
		}
		if val4.Formatted != "IDLE" {
			t.Errorf("expected NoValue IDLE, got %q", val4.Formatted)
		}

		// Panel 5 has no targets
		val5, err := client.GetPanelValue("app-deep-dive", 5)
		if err != nil {
			t.Fatalf("GetPanelValue(5) failed: %v", err)
		}
		if !strings.Contains(val5.Formatted, "N/A") {
			t.Errorf("expected N/A for text panel without queries, got %q", val5.Formatted)
		}
	})

	t.Run("QueryPromQL_Variants", func(t *testing.T) {
		// Default datasource vector with labels
		res, err := client.QueryPromQL("", "cpu_seconds_total")
		if err != nil {
			t.Fatalf("QueryPromQL vector failed: %v", err)
		}
		if res.IsScalar || len(res.VectorRows) != 1 {
			t.Fatalf("expected 1 vector row, got %+v", res)
		}
		if res.VectorRows[0].Labels != `{instance="10.0.0.1:8080", job="api-server"}` {
			t.Errorf("unexpected labels: %s", res.VectorRows[0].Labels)
		}

		// Named datasource scalar
		scalarRes, err := client.QueryPromQL("Thanos-Prod", "up")
		if err != nil {
			t.Fatalf("QueryPromQL Thanos-Prod failed: %v", err)
		}
		if !scalarRes.IsScalar || scalarRes.ScalarVal != "42" {
			t.Errorf("expected scalar 42, got %+v", scalarRes)
		}

		// Legacy ID-based datasource without UID
		legacyRes, err := client.QueryPromQL("victoriametrics", "up")
		if err != nil {
			t.Fatalf("QueryPromQL legacy ID failed: %v", err)
		}
		if !legacyRes.IsScalar || legacyRes.ScalarVal != "99" {
			t.Errorf("expected scalar 99, got %+v", legacyRes)
		}

		// Loki datasource with query_range fallback and streams parsing
		lokiRes, err := client.QueryPromQL("${DS_LOKI}", `{app="nginx"}`)
		if err != nil {
			t.Fatalf("QueryPromQL Loki failed: %v", err)
		}
		if len(lokiRes.VectorRows) != 1 || lokiRes.VectorRows[0].Value != "connection refused" {
			t.Errorf("unexpected Loki result: %+v", lokiRes)
		}

		// Query error handling
		if _, err := client.QueryPromQL("", "bad_query"); err == nil {
			t.Error("expected error for bad_query")
		}

		// Non-existent datasource
		if _, err := client.QueryPromQL("non-existent-ds", "up"); err == nil {
			t.Error("expected error for non-existent datasource")
		}
	})

	t.Run("GetFiringAlerts", func(t *testing.T) {
		alerts, err := client.GetFiringAlerts()
		if err != nil {
			t.Fatalf("GetFiringAlerts failed: %v", err)
		}
		if len(alerts) != 2 {
			t.Fatalf("expected 2 firing alerts, got %d", len(alerts))
		}
		if alerts[0].Name != "HighMemory" || alerts[0].Summary != "Memory > 90%" {
			t.Errorf("unexpected first alert: %+v", alerts[0])
		}
		if alerts[1].Name != "unnamed-alert" || alerts[1].Summary != "Fallback description" {
			t.Errorf("unexpected second alert: %+v", alerts[1])
		}
	})

	t.Run("CheckDataSourcesHealth", func(t *testing.T) {
		health, err := client.CheckDataSourcesHealth()
		if err != nil {
			t.Fatalf("CheckDataSourcesHealth failed: %v", err)
		}
		if len(health) != 3 {
			t.Fatalf("expected 3 health entries, got %d", len(health))
		}
		if !health[0].IsOK || health[0].Status != "OK" {
			t.Errorf("expected Prometheus to be OK, got %+v", health[0])
		}
		if health[1].IsOK || health[1].Status != "ERROR" {
			t.Errorf("expected Loki to fail health check, got %+v", health[1])
		}
		if !health[2].IsOK || health[2].Status != "OK" {
			t.Errorf("expected LegacyVM plain 200 to be OK, got %+v", health[2])
		}
	})

	t.Run("UnauthorizedClient", func(t *testing.T) {
		badClient := NewClient(srv.URL, "wrong-token")
		if _, err := badClient.ListDashboards(); err == nil || !strings.Contains(err.Error(), "401") {
			t.Errorf("expected 401 error with wrong token, got %v", err)
		}
	})
}

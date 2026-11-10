package grafana

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Client wraps the standard net/http client for Grafana API requests.
type Client struct {
	BaseURL    string
	Token      string
	httpClient *http.Client
}

// Alert represents a minimal firing alert in Grafana.
type Alert struct {
	Name    string
	Summary string
	State   string
}

// DashboardSummary represents a single row in `ocli dashboard ls`.
type DashboardSummary struct {
	UID    string
	Title  string
	Folder string
	Tags   []string
}

// PanelSummary represents a single row in `ocli pnl ls <DASH_UID>`.
type PanelSummary struct {
	ID          int
	Type        string
	Title       string
	Description string
}

// PanelQuery represents a query target inside a panel.
type PanelQuery struct {
	RefID string
	Expr  string
}

// PanelInfo represents detailed configuration for `ocli pnl info <DASH_UID> <PANEL_ID>`.
type PanelInfo struct {
	ID           int
	Title        string
	DashboardUID string
	Type         string
	DataSource   string
	Queries      []PanelQuery
	Transforms   []string
}

// PanelValue represents the evaluated value of a panel for `ocli pnl <DASH_UID> <PANEL_ID>`.
type PanelValue struct {
	Title     string
	Formatted string
	ColorANSI string
}

// PromQLResult represents the output of `ocli pql "<query>"`.
type PromQLResult struct {
	IsScalar   bool
	ScalarVal  string
	VectorRows []PromQLRow
}

// PromQLRow represents a single metric value + label set.
type PromQLRow struct {
	Value  string
	Labels string
}

// NewClient initializes a minimal Grafana HTTP client with a 10s timeout.
func NewClient(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) doRequest(method, endpoint string, payload []byte) ([]byte, error) {
	reqURL := fmt.Sprintf("%s%s", c.BaseURL, endpoint)
	var bodyReader io.Reader
	if payload != nil {
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequest(method, reqURL, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("grafana API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return body, nil
}

func (c *Client) doGet(endpoint string) ([]byte, error) {
	return c.doRequest(http.MethodGet, endpoint, nil)
}

// internal raw structures for decoding Grafana dashboard JSON
type rawPanel struct {
	ID          int             `json:"id"`
	Title       string          `json:"title"`
	Type        string          `json:"type"`
	Description string          `json:"description"`
	DataSource  json.RawMessage `json:"datasource"`
	Targets     []struct {
		RefID      string          `json:"refId"`
		Expr       string          `json:"expr"`
		Query      string          `json:"query"`
		DataSource json.RawMessage `json:"datasource"`
	} `json:"targets"`
	Transformations []struct {
		ID      string                 `json:"id"`
		Options map[string]interface{} `json:"options"`
	} `json:"transformations"`
	FieldConfig struct {
		Defaults struct {
			Unit    string `json:"unit"`
			NoValue string `json:"noValue"`
			Color   struct {
				Mode       string `json:"mode"`
				FixedColor string `json:"fixedColor"`
			} `json:"color"`
			Thresholds struct {
				Mode  string `json:"mode"`
				Steps []struct {
					Color string   `json:"color"`
					Value *float64 `json:"value"`
				} `json:"steps"`
			} `json:"thresholds"`
			Mappings []struct {
				Type    string `json:"type"`
				Options map[string]struct {
					Text  string `json:"text"`
					Color string `json:"color"`
					Index int    `json:"index"`
				} `json:"options"`
			} `json:"mappings"`
		} `json:"defaults"`
	} `json:"fieldConfig"`
	// Legacy singlestat / gauge fields
	Thresholds string     `json:"thresholds"`
	Colors     []string   `json:"colors"`
	Panels     []rawPanel `json:"panels"`
}

type rawTemplateVar struct {
	Name       string `json:"name"`
	AllValue   string `json:"allValue"`
	IncludeAll bool   `json:"includeAll"`
	Current    struct {
		Value json.RawMessage `json:"value"`
	} `json:"current"`
}

func flattenPanels(panels []rawPanel) []rawPanel {
	var out []rawPanel
	for _, p := range panels {
		if p.Type == "row" && len(p.Panels) > 0 {
			out = append(out, flattenPanels(p.Panels)...)
			continue
		}
		out = append(out, p)
		if len(p.Panels) > 0 {
			out = append(out, flattenPanels(p.Panels)...)
		}
	}
	return out
}

func parseDataSourceRef(raw json.RawMessage) (uidOrName string) {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str
	}
	var obj struct {
		UID  string `json:"uid"`
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		if obj.Name != "" {
			return obj.Name
		}
		if obj.UID != "" {
			return obj.UID
		}
		return obj.Type
	}
	return ""
}

func (c *Client) fetchDashboard(dashboardUID string) ([]rawPanel, map[string]string, error) {
	body, err := c.doGet(fmt.Sprintf("/api/dashboards/uid/%s", url.PathEscape(dashboardUID)))
	if err != nil {
		return nil, nil, err
	}

	var result struct {
		Dashboard struct {
			Panels     []rawPanel `json:"panels"`
			Templating struct {
				List []rawTemplateVar `json:"list"`
			} `json:"templating"`
		} `json:"dashboard"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, nil, fmt.Errorf("failed to parse dashboard JSON: %w", err)
	}

	vars := buildTemplateVarsMap(result.Dashboard.Templating.List)
	return flattenPanels(result.Dashboard.Panels), vars, nil
}

func buildTemplateVarsMap(list []rawTemplateVar) map[string]string {
	vars := map[string]string{
		"__rate_interval": "5m",
		"__interval":      "1m",
		"__interval_ms":   "60000",
		"__range":         "1h",
		"__range_s":       "3600",
		"__range_ms":      "3600000",
	}

	for _, v := range list {
		if v.Name == "" {
			continue
		}
		var val string
		if len(v.Current.Value) > 0 && string(v.Current.Value) != "null" {
			var s string
			if err := json.Unmarshal(v.Current.Value, &s); err == nil {
				val = s
			} else {
				var arr []string
				if err := json.Unmarshal(v.Current.Value, &arr); err == nil && len(arr) > 0 {
					if arr[0] == "$__all" {
						val = "$__all"
					} else {
						val = strings.Join(arr, "|")
					}
				}
			}
		}

		if val == "" || val == "$__all" {
			if v.AllValue != "" {
				val = v.AllValue
			} else if v.IncludeAll {
				val = ".*"
			}
		}

		if val != "" {
			vars[v.Name] = val
		}
	}

	return vars
}

func interpolateExpr(expr string, vars map[string]string) string {
	if len(vars) == 0 || !strings.Contains(expr, "$") {
		return expr
	}

	// Sort keys by length descending so longer variable names ($service_name) are replaced before shorter prefixes ($service).
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return len(keys[i]) > len(keys[j])
	})

	out := expr
	for _, k := range keys {
		val := vars[k]
		out = strings.ReplaceAll(out, "${"+k+":regex}", val)
		out = strings.ReplaceAll(out, "${"+k+":pipe}", val)
		out = strings.ReplaceAll(out, "${"+k+":raw}", val)
		out = strings.ReplaceAll(out, "${"+k+"}", val)
		out = strings.ReplaceAll(out, "$"+k, val)
	}
	return out
}

// ListPanels returns all panels in a dashboard for `ocli pnl ls <DASH_UID>`.
func (c *Client) ListPanels(dashboardUID string) ([]PanelSummary, error) {
	panels, _, err := c.fetchDashboard(dashboardUID)
	if err != nil {
		return nil, err
	}

	var summaries []PanelSummary
	for _, p := range panels {
		if p.Type == "row" {
			continue
		}
		summaries = append(summaries, PanelSummary{
			ID:          p.ID,
			Type:        p.Type,
			Title:       p.Title,
			Description: strings.ReplaceAll(strings.TrimSpace(p.Description), "\n", " "),
		})
	}
	return summaries, nil
}

// GetPanelInfo returns detailed panel settings for `ocli pnl info <DASH_UID> <PANEL_ID>`.
func (c *Client) GetPanelInfo(dashboardUID string, panelID int) (*PanelInfo, error) {
	panels, _, err := c.fetchDashboard(dashboardUID)
	if err != nil {
		return nil, err
	}

	for _, p := range panels {
		if p.ID != panelID {
			continue
		}

		ds := parseDataSourceRef(p.DataSource)
		var queries []PanelQuery
		for _, t := range p.Targets {
			if ds == "" {
				ds = parseDataSourceRef(t.DataSource)
			}
			expr := strings.TrimSpace(t.Expr)
			if expr == "" {
				expr = strings.TrimSpace(t.Query)
			}
			if expr != "" {
				ref := t.RefID
				if ref == "" {
					ref = "A"
				}
				queries = append(queries, PanelQuery{
					RefID: ref,
					Expr:  expr,
				})
			}
		}
		if ds == "" {
			ds = "default"
		}

		var transforms []string
		for _, tr := range p.Transformations {
			desc := tr.ID
			if tr.ID == "organize" {
				if excludeByName, ok := tr.Options["excludeByName"].(map[string]interface{}); ok {
					var hidden []string
					for field, excluded := range excludeByName {
						if b, ok := excluded.(bool); ok && b {
							hidden = append(hidden, field)
						}
					}
					sort.Strings(hidden)
					if len(hidden) > 0 {
						desc = fmt.Sprintf("Organize fields (Hidden: %s)", strings.Join(hidden, ", "))
					} else {
						desc = "Organize fields"
					}
				}
			}
			transforms = append(transforms, desc)
		}

		return &PanelInfo{
			ID:           p.ID,
			Title:        p.Title,
			DashboardUID: dashboardUID,
			Type:         p.Type,
			DataSource:   ds,
			Queries:      queries,
			Transforms:   transforms,
		}, nil
	}

	return nil, fmt.Errorf("panel %d not found in dashboard %s", panelID, dashboardUID)
}

// GetPanelValue evaluates the panel's primary query and returns a minimal status-colored value.
func (c *Client) GetPanelValue(dashboardUID string, panelID int) (*PanelValue, error) {
	panels, vars, err := c.fetchDashboard(dashboardUID)
	if err != nil {
		return nil, err
	}

	for _, p := range panels {
		if p.ID != panelID {
			continue
		}

		ds := parseDataSourceRef(p.DataSource)
		var expr string
		for _, t := range p.Targets {
			if ds == "" {
				ds = parseDataSourceRef(t.DataSource)
			}
			if t.Expr != "" {
				expr = t.Expr
				break
			}
			if t.Query != "" {
				expr = t.Query
				break
			}
		}

		if expr == "" {
			return &PanelValue{
				Title:     p.Title,
				Formatted: "N/A (no query expression)",
			}, nil
		}

		interpolated := interpolateExpr(expr, vars)
		res, err := c.QueryPromQL(ds, interpolated)
		if err != nil {
			return nil, err
		}

		var rawVal string
		hasData := false
		if res.IsScalar {
			rawVal = res.ScalarVal
			hasData = true
		} else if len(res.VectorRows) > 0 {
			rawVal = res.VectorRows[0].Value
			hasData = true
		}

		var formatted string
		if hasData {
			formatted = formatWithUnit(rawVal, p.FieldConfig.Defaults.Unit)
		} else if p.FieldConfig.Defaults.NoValue != "" {
			rawVal = p.FieldConfig.Defaults.NoValue
			formatted = p.FieldConfig.Defaults.NoValue
		} else {
			rawVal = "No data"
			formatted = "No data"
		}

		colorANSI := resolvePanelColorANSI(rawVal, p)

		return &PanelValue{
			Title:     p.Title,
			Formatted: formatted,
			ColorANSI: colorANSI,
		}, nil
	}

	return nil, fmt.Errorf("panel %d not found in dashboard %s", panelID, dashboardUID)
}

func formatNumberSmart(num float64) string {
	if num == 0 {
		return "0"
	}
	if num == float64(int64(num)) {
		return fmt.Sprintf("%d", int64(num))
	}
	abs := num
	if abs < 0 {
		abs = -abs
	}
	if abs < 1 {
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", num), "0"), ".")
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", num), "0"), ".")
}

func formatWithUnit(rawVal, unit string) string {
	num, err := strconv.ParseFloat(rawVal, 64)
	if err != nil {
		return rawVal
	}

	switch unit {
	case "reqps":
		return fmt.Sprintf("%s req/s", formatNumberSmart(num))
	case "ops":
		return fmt.Sprintf("%s ops/s", formatNumberSmart(num))
	case "rps":
		return fmt.Sprintf("%s r/s", formatNumberSmart(num))
	case "wps":
		return fmt.Sprintf("%s W/s", formatNumberSmart(num))
	case "iops":
		return fmt.Sprintf("%s iops", formatNumberSmart(num))
	case "percent", "percentunit":
		if unit == "percentunit" {
			num *= 100
		}
		if num == 0 {
			return "0%"
		}
		return fmt.Sprintf("%s%%", formatNumberSmart(num))
	case "bytes", "decbytes":
		return fmt.Sprintf("%s B", formatNumberSmart(num))
	case "s", "sec":
		return fmt.Sprintf("%ss", formatNumberSmart(num))
	case "ms":
		return fmt.Sprintf("%sms", formatNumberSmart(num))
	default:
		return formatNumberSmart(num)
	}
}

func resolvePanelColorANSI(rawVal string, p rawPanel) string {
	// 1. Check Value Mappings first (e.g., 1 -> green, 0 -> red)
	for _, m := range p.FieldConfig.Defaults.Mappings {
		if m.Type == "value" {
			if opt, ok := m.Options[rawVal]; ok && opt.Color != "" {
				if ansi := grafanaColorToANSI(opt.Color); ansi != "" {
					return ansi
				}
			}
		}
	}

	// 2. Check fixed color mode
	if p.FieldConfig.Defaults.Color.Mode == "fixed" && p.FieldConfig.Defaults.Color.FixedColor != "" {
		if ansi := grafanaColorToANSI(p.FieldConfig.Defaults.Color.FixedColor); ansi != "" {
			return ansi
		}
	}

	// 3. Check modern thresholds steps
	steps := p.FieldConfig.Defaults.Thresholds.Steps
	if len(steps) > 0 {
		matchedColor := steps[0].Color
		if num, err := strconv.ParseFloat(rawVal, 64); err == nil {
			for _, s := range steps {
				if s.Value != nil && num >= *s.Value {
					matchedColor = s.Color
				}
			}
		}
		if ansi := grafanaColorToANSI(matchedColor); ansi != "" {
			return ansi
		}
	}

	// 4. Check legacy singlestat thresholds ("70,90" + colors array)
	if p.Thresholds != "" && len(p.Colors) > 0 {
		matchedColor := p.Colors[0]
		if num, err := strconv.ParseFloat(rawVal, 64); err == nil {
			parts := strings.Split(p.Thresholds, ",")
			for i, part := range parts {
				if th, err := strconv.ParseFloat(strings.TrimSpace(part), 64); err == nil && num >= th {
					if i+1 < len(p.Colors) {
						matchedColor = p.Colors[i+1]
					}
				}
			}
		}
		if ansi := grafanaColorToANSI(matchedColor); ansi != "" {
			return ansi
		}
	}

	// 5. Fallback to Grafana's default base green if value is numeric
	if _, err := strconv.ParseFloat(rawVal, 64); err == nil {
		return grafanaColorToANSI("green")
	}

	return ""
}

// grafanaColorToANSI converts Grafana named colors, #HEX, or rgb()/rgba() to 24-bit TrueColor ANSI escape sequences.
func grafanaColorToANSI(c string) string {
	c = strings.TrimSpace(strings.ToLower(c))
	if c == "" || c == "transparent" || c == "text" {
		return ""
	}

	// Named Grafana UI palette colors -> exact Grafana RGB values
	palette := map[string][3]int{
		"green":              {115, 191, 105}, // #73BF69
		"super-light-green":  {200, 242, 194},
		"light-green":        {150, 217, 141},
		"semi-dark-green":    {86, 166, 75},
		"dark-green":         {55, 135, 45},
		"yellow":             {250, 222, 42}, // #FADE2A
		"super-light-yellow": {255, 248, 153},
		"light-yellow":       {252, 238, 92},
		"semi-dark-yellow":   {224, 180, 0},
		"dark-yellow":        {196, 140, 0},
		"orange":             {255, 152, 48}, // #FF9830
		"super-light-orange": {255, 203, 125},
		"light-orange":       {255, 179, 87},
		"semi-dark-orange":   {250, 100, 0},
		"dark-orange":        {229, 84, 0},
		"red":                {242, 73, 92}, // #F2495C
		"super-light-red":    {255, 166, 176},
		"light-red":          {255, 115, 131},
		"semi-dark-red":      {224, 47, 68},
		"dark-red":           {196, 22, 42},
		"blue":               {87, 148, 242}, // #5794F2
		"super-light-blue":   {192, 216, 255},
		"light-blue":         {138, 184, 255},
		"semi-dark-blue":     {50, 116, 217},
		"dark-blue":          {31, 96, 196},
		"purple":             {184, 119, 217}, // #B877D9
		"super-light-purple": {222, 182, 242},
		"light-purple":       {202, 149, 229},
		"semi-dark-purple":   {163, 82, 204},
		"dark-purple":        {143, 59, 184},
	}

	if rgb, ok := palette[c]; ok {
		return fmt.Sprintf("\033[38;2;%d;%d;%dm", rgb[0], rgb[1], rgb[2])
	}

	// Hex color (#RRGGBB or #RGB)
	if strings.HasPrefix(c, "#") {
		hex := strings.TrimPrefix(c, "#")
		if len(hex) == 3 {
			hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
		}
		if len(hex) >= 6 {
			r, err1 := strconv.ParseInt(hex[0:2], 16, 64)
			g, err2 := strconv.ParseInt(hex[2:4], 16, 64)
			b, err3 := strconv.ParseInt(hex[4:6], 16, 64)
			if err1 == nil && err2 == nil && err3 == nil {
				return fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b)
			}
		}
	}

	// rgb(r, g, b) or rgba(r, g, b, a)
	if strings.HasPrefix(c, "rgb") {
		open := strings.IndexByte(c, '(')
		close := strings.LastIndexByte(c, ')')
		if open != -1 && close > open {
			parts := strings.Split(c[open+1:close], ",")
			if len(parts) >= 3 {
				r, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
				g, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
				b, err3 := strconv.Atoi(strings.TrimSpace(parts[2]))
				if err1 == nil && err2 == nil && err3 == nil {
					return fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b)
				}
			}
		}
	}

	return ""
}

type dsInfo struct {
	ID        int    `json:"id"`
	UID       string `json:"uid"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	IsDefault bool   `json:"isDefault"`
}

func (c *Client) resolveDataSource(identifier string) (*dsInfo, error) {
	identifier = strings.TrimSpace(identifier)

	// 1. If specific name or UID is given, try by name then by UID.
	if identifier != "" && identifier != "default" && identifier != "$datasource" {
		if body, err := c.doGet("/api/datasources/name/" + url.PathEscape(identifier)); err == nil {
			var ds dsInfo
			if json.Unmarshal(body, &ds) == nil && (ds.UID != "" || ds.ID != 0) {
				return &ds, nil
			}
		}
		if body, err := c.doGet("/api/datasources/uid/" + url.PathEscape(identifier)); err == nil {
			var ds dsInfo
			if json.Unmarshal(body, &ds) == nil && (ds.UID != "" || ds.ID != 0) {
				return &ds, nil
			}
		}
		return nil, fmt.Errorf("datasource %q not found in Grafana", identifier)
	}

	// 2. Otherwise fetch all datasources and pick default (or first prometheus-compatible one).
	body, err := c.doGet("/api/datasources")
	if err != nil {
		return nil, err
	}
	var list []dsInfo
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("failed to parse datasources list: %w", err)
	}

	for _, ds := range list {
		if ds.IsDefault {
			return &ds, nil
		}
	}
	for _, ds := range list {
		if strings.Contains(strings.ToLower(ds.Type), "prometheus") {
			return &ds, nil
		}
	}
	if len(list) > 0 {
		return &list[0], nil
	}

	return nil, fmt.Errorf("no datasources configured in Grafana")
}

// QueryPromQL executes an instant PromQL query via Grafana's DataSource proxy.
func (c *Client) QueryPromQL(dsIdentifier, query string) (*PromQLResult, error) {
	ds, err := c.resolveDataSource(dsIdentifier)
	if err != nil {
		return nil, err
	}

	var proxyPath string
	if ds.UID != "" {
		proxyPath = fmt.Sprintf("/api/datasources/proxy/uid/%s/api/v1/query?query=%s", url.PathEscape(ds.UID), url.QueryEscape(query))
	} else {
		proxyPath = fmt.Sprintf("/api/datasources/proxy/%d/api/v1/query?query=%s", ds.ID, url.QueryEscape(query))
	}

	body, err := c.doGet(proxyPath)
	if err != nil {
		return nil, err
	}

	var promResp struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			ResultType string          `json:"resultType"`
			Result     json.RawMessage `json:"result"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &promResp); err != nil {
		return nil, fmt.Errorf("failed to parse PromQL response: %w", err)
	}
	if promResp.Status != "success" {
		return nil, fmt.Errorf("promql error: %s", promResp.Error)
	}

	switch promResp.Data.ResultType {
	case "scalar", "string":
		var pair []interface{}
		if err := json.Unmarshal(promResp.Data.Result, &pair); err != nil || len(pair) < 2 {
			return nil, fmt.Errorf("unexpected scalar format")
		}
		return &PromQLResult{
			IsScalar:  true,
			ScalarVal: fmt.Sprintf("%v", pair[1]),
		}, nil

	case "vector":
		var samples []struct {
			Metric map[string]string `json:"metric"`
			Value  []interface{}     `json:"value"`
		}
		if err := json.Unmarshal(promResp.Data.Result, &samples); err != nil {
			return nil, fmt.Errorf("failed to parse vector samples: %w", err)
		}

		var rows []PromQLRow
		for _, s := range samples {
			val := ""
			if len(s.Value) >= 2 {
				val = fmt.Sprintf("%v", s.Value[1])
			}
			rows = append(rows, PromQLRow{
				Value:  val,
				Labels: formatLabels(s.Metric),
			})
		}
		return &PromQLResult{
			IsScalar:   false,
			VectorRows: rows,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported PromQL result type: %s", promResp.Data.ResultType)
	}
}

func formatLabels(m map[string]string) string {
	if len(m) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", k, m[k]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// GetFiringAlerts fetches all currently firing alerts from Grafana Unified Alerting.
func (c *Client) GetFiringAlerts() ([]Alert, error) {
	body, err := c.doGet("/api/prometheus/grafana/api/v1/alerts")
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			Alerts []struct {
				Labels      map[string]string `json:"labels"`
				Annotations map[string]string `json:"annotations"`
				State       string            `json:"state"`
			} `json:"alerts"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse alerts response: %w", err)
	}

	var firing []Alert
	for _, a := range resp.Data.Alerts {
		if strings.EqualFold(a.State, "firing") {
			name := a.Labels["alertname"]
			if name == "" {
				name = "unnamed-alert"
			}
			summary := a.Annotations["summary"]
			if summary == "" {
				summary = a.Annotations["description"]
			}
			firing = append(firing, Alert{
				Name:    name,
				Summary: summary,
				State:   a.State,
			})
		}
	}

	return firing, nil
}

// ListDashboards fetches all dashboards via `/api/search?type=dash-db`.
func (c *Client) ListDashboards() ([]DashboardSummary, error) {
	body, err := c.doGet("/api/search?type=dash-db")
	if err != nil {
		return nil, err
	}

	var items []struct {
		UID         string   `json:"uid"`
		Title       string   `json:"title"`
		FolderTitle string   `json:"folderTitle"`
		Tags        []string `json:"tags"`
	}

	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse dashboards list: %w", err)
	}

	var out []DashboardSummary
	for _, item := range items {
		folder := strings.TrimSpace(item.FolderTitle)
		if folder == "" {
			folder = "General"
		}
		out = append(out, DashboardSummary{
			UID:    item.UID,
			Title:  item.Title,
			Folder: folder,
			Tags:   item.Tags,
		})
	}

	return out, nil
}


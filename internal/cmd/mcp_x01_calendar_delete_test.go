package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"google.golang.org/api/calendar/v3"

	"github.com/steipete/gogcli/internal/app"
)

func TestMCPCalendarDeleteEventBuildArgsExact(t *testing.T) {
	tests := []struct {
		name      string
		arguments map[string]any
		want      []string
	}{
		{
			name: "all defaults notifications",
			arguments: map[string]any{
				"calendar_id": "primary",
				"event_id":    "event-1",
				"scope":       "all",
			},
			want: []string{"calendar", "delete", "--force", "--scope", "all", "--send-updates", "none", "--", "primary", "event-1"},
		},
		{
			name: "single recurrence",
			arguments: map[string]any{
				"calendar_id":    "primary",
				"event_id":       "series-1",
				"scope":          "single",
				"original_start": "not parsed by adapter",
			},
			want: []string{"calendar", "delete", "--force", "--scope", "single", "--send-updates", "none", "--original-start", "not parsed by adapter", "--", "primary", "series-1"},
		},
		{
			name: "future external notifications",
			arguments: map[string]any{
				"calendar_id":    "calendar@example.com",
				"event_id":       "series-2",
				"scope":          "future",
				"original_start": "2026-08-04T09:00:00Z",
				"send_updates":   "externalOnly",
			},
			want: []string{"calendar", "delete", "--force", "--scope", "future", "--send-updates", "externalOnly", "--original-start", "2026-08-04T09:00:00Z", "--", "calendar@example.com", "series-2"},
		},
		{
			name: "all notifications",
			arguments: map[string]any{
				"calendar_id":  "primary",
				"event_id":     "event-3",
				"scope":        "all",
				"send_updates": "all",
			},
			want: []string{"calendar", "delete", "--force", "--scope", "all", "--send-updates", "all", "--", "primary", "event-3"},
		},
	}

	tool := findMCPTool(t, "calendar_delete_event")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := tool.BuildArgs(calendarMCPRequest(tt.arguments))
			if err != nil {
				t.Fatalf("BuildArgs: %v", err)
			}
			if got := strings.Join(args, "\x00"); got != strings.Join(tt.want, "\x00") {
				t.Fatalf("args = %#v, want %#v", args, tt.want)
			}
			if !containsMCPArg(args, "--force") {
				t.Fatal("server-controlled --force missing from child argv")
			}
			if _, supplied := tt.arguments["force"]; supplied {
				t.Fatal("force must not be a model input")
			}
		})
	}
}

func TestMCPCalendarDeleteEventRejectsInvalidRecurrenceCombinations(t *testing.T) {
	base := map[string]any{
		"calendar_id": "primary",
		"event_id":    "event-1",
		"scope":       "all",
	}
	tests := []struct {
		name string
		add  map[string]any
		want string
	}{
		{name: "missing calendar id", add: map[string]any{"calendar_id": nil}, want: "calendar_id"},
		{name: "empty calendar id", add: map[string]any{"calendar_id": ""}, want: "calendar_id"},
		{name: "missing event id", add: map[string]any{"event_id": nil}, want: "event_id"},
		{name: "empty event id", add: map[string]any{"event_id": ""}, want: "event_id"},
		{name: "missing scope", add: map[string]any{"scope": nil}, want: "scope"},
		{name: "empty scope", add: map[string]any{"scope": ""}, want: "scope"},
		{name: "invalid scope", add: map[string]any{"scope": "series"}, want: "scope"},
		{name: "single requires original start", add: map[string]any{"scope": "single"}, want: "original_start"},
		{name: "future requires original start", add: map[string]any{"scope": "future"}, want: "original_start"},
		{name: "all rejects original start", add: map[string]any{"original_start": "2026-08-04T09:00:00Z"}, want: "original_start"},
		{name: "all rejects empty original start", add: map[string]any{"original_start": ""}, want: "original_start"},
		{name: "invalid notifications", add: map[string]any{"send_updates": "notifyEveryone"}, want: "send_updates"},
		{name: "empty notifications", add: map[string]any{"send_updates": ""}, want: "send_updates"},
		{name: "wrong original start type", add: map[string]any{"scope": "single", "original_start": 12}, want: "original_start"},
		{name: "wrong notifications type", add: map[string]any{"send_updates": true}, want: "send_updates"},
	}

	tool := findMCPTool(t, "calendar_delete_event")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arguments := cloneMCPX01Arguments(base)
			for key, value := range tt.add {
				if value == nil {
					delete(arguments, key)
					continue
				}
				arguments[key] = value
			}
			_, err := tool.BuildArgs(calendarMCPRequest(arguments))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("BuildArgs error = %v, want text containing %q", err, tt.want)
			}
		})
	}
}

func TestMCPCalendarDeleteEventSchemaAndPolicy(t *testing.T) {
	spec := findMCPTool(t, "calendar_delete_event")
	if spec.Risk != mcpRiskDestructive {
		t.Fatalf("risk = %q, want %q", spec.Risk, mcpRiskDestructive)
	}
	tool := newMCPTool(spec)
	closed, ok := tool.InputSchema.AdditionalProperties.(bool)
	if !ok || closed {
		t.Fatalf("schema additionalProperties = %#v, want false", tool.InputSchema.AdditionalProperties)
	}
	wantProperties := map[string]bool{
		"calendar_id": true, "event_id": true, "scope": true, "original_start": true, "send_updates": true,
	}
	if len(tool.InputSchema.Properties) != len(wantProperties) {
		t.Fatalf("schema properties = %#v, want exactly %#v", tool.InputSchema.Properties, wantProperties)
	}
	for field := range wantProperties {
		if _, present := tool.InputSchema.Properties[field]; !present {
			t.Errorf("schema missing %q", field)
		}
	}
	for _, forbidden := range []string{"force", "dry_run", "argv", "query", "max", "calendar_ids", "calendars", "path", "stdin", "file"} {
		if _, present := tool.InputSchema.Properties[forbidden]; present {
			t.Errorf("forbidden schema field %q exposed", forbidden)
		}
	}
	if got := strings.Join(tool.InputSchema.Required, "\x00"); got != strings.Join([]string{"calendar_id", "event_id", "scope"}, "\x00") {
		t.Fatalf("required fields = %#v, want [calendar_id event_id scope]", tool.InputSchema.Required)
	}

	for _, selector := range []string{"", "write", "calendar", "calendar.*", "all", "*"} {
		cmd := McpCmd{AllowWrite: true}
		if selector != "" {
			cmd.AllowTool = []string{selector}
		}
		if hasMCPTool(mcpEnabledTools(cmd), spec.Name) {
			t.Fatalf("ordinary/broad selector %q exposed destructive tool", selector)
		}
	}
	if hasMCPTool(mcpEnabledTools(McpCmd{AllowTool: []string{"destructive"}}), spec.Name) {
		t.Fatal("destructive selector exposed tool without ordinary write authorization")
	}
	for _, selector := range []string{"destructive", spec.Name} {
		if !hasMCPTool(mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{selector}}), spec.Name) {
			t.Fatalf("authorized destructive selector %q did not expose tool", selector)
		}
	}
	readonly := mcpEnabledToolsNoPolicy(McpCmd{AllowWrite: true, AllowTool: []string{"destructive"}}, &RootFlags{ReadOnly: true})
	if hasMCPTool(readonly, spec.Name) {
		t.Fatal("readonly root exposed destructive tool")
	}
}

func TestMCPCalendarDeleteEventServerRejectsUnknownSchemaFields(t *testing.T) {
	calls := map[string]int{}
	client := newCalendarMCPValidationClient(t, []string{"calendar_delete_event"}, calls)
	base := map[string]any{"calendar_id": "primary", "event_id": "event-1", "scope": "all"}
	for _, field := range []string{"force", "argv", "query", "calendar_ids", "all"} {
		t.Run(field, func(t *testing.T) {
			arguments := cloneMCPX01Arguments(base)
			arguments[field] = true
			before := calls["calendar_delete_event"]
			result, err := client.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{
				Name: "calendar_delete_event", Arguments: arguments,
			}})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError || calls["calendar_delete_event"] != before {
				t.Fatalf("result = %#v, calls = %d (before %d), want schema rejection before handler", result.Content, calls["calendar_delete_event"], before)
			}
			if !strings.Contains(mcpResultText(result), field) {
				t.Fatalf("result = %#v, want field %q in validation error", result.Content, field)
			}
		})
	}
}

func TestMCPCalendarDeleteEventProviderFixture(t *testing.T) {
	var deleteCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/calendar/v3")
		if r.Method != http.MethodDelete || path != "/calendars/primary/events/event-1" {
			http.NotFound(w, r)
			return
		}
		deleteCalls++
		if got := r.URL.Query().Get("sendUpdates"); got != "all" {
			t.Errorf("sendUpdates = %q, want all", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	svc := newCalendarServiceFromServer(t, srv)
	var output bytes.Buffer
	ctx := withCalendarTestService(newCmdRuntimeJSONOutputContext(t, &output, io.Discard), svc)
	cmd := CalendarDeleteCmd{CalendarID: "primary", EventID: "event-1", Scope: scopeAll, SendUpdates: "all"}
	if err := cmd.Run(ctx, &RootFlags{Account: "a@b.com", Force: true}); err != nil {
		t.Fatalf("CalendarDeleteCmd: %v", err)
	}
	if deleteCalls != 1 {
		t.Fatalf("provider delete calls = %d, want 1", deleteCalls)
	}
	var result struct {
		Deleted    bool   `json:"deleted"`
		CalendarID string `json:"calendarId"`
		EventID    string `json:"eventId"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("decode structured delete result: %v", err)
	}
	if !result.Deleted || result.CalendarID != "primary" || result.EventID != "event-1" {
		t.Fatalf("structured delete result = %#v", result)
	}
}

func TestMCPCalendarDeleteEventRunnerReturnsStructuredDryRunResult(t *testing.T) {
	t.Setenv("GOG_MCP_X01_DELETE_RUNNER_HELPER", "1")
	tool := findMCPTool(t, "calendar_delete_event")
	commandArgs, err := tool.BuildArgs(calendarMCPRequest(map[string]any{
		"calendar_id": "primary",
		"event_id":    "event-1",
		"scope":       "all",
	}))
	if err != nil {
		t.Fatalf("BuildArgs: %v", err)
	}
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self: os.Args[0],
		tool: tool,
		baseArgs: append(
			[]string{"-test.run=TestMCPCalendarDeleteEventRunnerHelper$", "--"},
			mcpParentRootArgs(&RootFlags{DryRun: true})...,
		),
		commandArgs:    commandArgs,
		timeout:        5 * time.Second,
		maxOutputBytes: 4096,
	})
	if result.IsError {
		t.Fatalf("runner result is error: %#v", result.StructuredContent)
	}
	got, ok := result.StructuredContent.(mcpCommandResult)
	if !ok {
		t.Fatalf("structured result type = %T, want mcpCommandResult", result.StructuredContent)
	}
	if got.Tool != "calendar_delete_event" || got.Service != "calendar" || got.Risk != string(mcpRiskDestructive) || got.ExitCode != 0 {
		t.Fatalf("runner metadata = %#v", got)
	}
	if got.Stderr != "calendar delete fixture stderr\n" {
		t.Fatalf("stderr = %q, want separate fixture diagnostics", got.Stderr)
	}
	stdout, ok := got.Stdout.(map[string]any)
	if !ok || stdout["op"] != "calendar.delete" {
		t.Fatalf("structured stdout = %#v, want dry-run operation", got.Stdout)
	}
	request, ok := stdout["request"].(map[string]any)
	if !ok || request["scope"] != "all" || request["event_id"] != "event-1" {
		t.Fatalf("dry-run request = %#v", stdout["request"])
	}
}

func TestMCPCalendarDeleteEventFuturePatchFailurePreservesDeletedInstance(t *testing.T) {
	t.Setenv("GOG_MCP_X01_FUTURE_FAILURE_HELPER", "1")
	tool := findMCPTool(t, "calendar_delete_event")
	commandArgs, err := tool.BuildArgs(calendarMCPRequest(map[string]any{
		"calendar_id":    "primary",
		"event_id":       "series-1",
		"scope":          "future",
		"original_start": "2026-08-04T09:00:00Z",
	}))
	if err != nil {
		t.Fatalf("BuildArgs: %v", err)
	}
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self: os.Args[0],
		tool: tool,
		baseArgs: []string{
			"-test.run=TestMCPCalendarDeleteEventFuturePatchFailureHelper$", "--",
			"--json", "--account", "mcp@example.com",
		},
		commandArgs:    commandArgs,
		timeout:        5 * time.Second,
		maxOutputBytes: 16 * 1024,
	})
	got, ok := result.StructuredContent.(mcpCommandResult)
	if !ok {
		t.Fatalf("structured result type = %T, want mcpCommandResult", result.StructuredContent)
	}
	if !result.IsError || got.ExitCode == 0 || !strings.Contains(got.Stderr, "synthetic recurrence patch failure") {
		t.Fatalf("future patch failure = %#v", got)
	}
	stdout, ok := got.Stdout.(map[string]any)
	if !ok {
		t.Fatalf("partial stdout type = %T, want object", got.Stdout)
	}
	if stdout["deleted"] != true || stdout["eventId"] != "instance-1" ||
		stdout["parentEventId"] != "series-1" || stdout["seriesUpdated"] != false {
		t.Fatalf("partial future-delete evidence = %#v", stdout)
	}
}

func TestMCPCalendarDeleteEventFuturePatchFailureHelper(t *testing.T) {
	if os.Getenv("GOG_MCP_X01_FUTURE_FAILURE_HELPER") != "1" {
		return
	}
	var deleteCalls, patchCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/calendar/v3")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && path == "/calendars/primary/events/series-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "series-1", "recurrence": []string{"RRULE:FREQ=DAILY"},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/calendars/primary/events/series-1/instances"):
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{
				"id":                "instance-1",
				"originalStartTime": map[string]any{"dateTime": "2026-08-04T09:00:00Z"},
			}}})
		case r.Method == http.MethodDelete && path == "/calendars/primary/events/instance-1":
			deleteCalls++
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPatch && path == "/calendars/primary/events/series-1":
			patchCalls++
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"message":"synthetic recurrence patch failure"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	svc := newCalendarServiceFromServer(t, srv)
	result := executeWithTestRuntime(t, mcpX02ChildCLIArgs(), &app.Runtime{Services: app.Services{
		Calendar: func(context.Context, string) (*calendar.Service, error) { return svc, nil },
	}})
	if deleteCalls != 1 || patchCalls != 1 {
		result.stderr += fmt.Sprintf("provider calls delete=%d patch=%d\n", deleteCalls, patchCalls)
	}
	mcpNativeEmitExecuteResult(result)
}

func TestMCPCalendarDeleteEventRunnerHelper(t *testing.T) {
	if os.Getenv("GOG_MCP_X01_DELETE_RUNNER_HELPER") != "1" {
		return
	}
	joined := strings.Join(os.Args[1:], "\x00")
	for _, required := range []string{"--dry-run", "--force", "calendar\x00delete\x00--force\x00--scope\x00all", "--\x00primary\x00event-1"} {
		if !strings.Contains(joined, required) {
			os.Exit(2)
		}
	}
	_, _ = io.WriteString(os.Stdout, `{"op":"calendar.delete","request":{"scope":"all","event_id":"event-1","send_updates":"none"}}`)
	_, _ = io.WriteString(os.Stderr, "calendar delete fixture stderr\n")
	os.Exit(0)
}

func cloneMCPX01Arguments(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/steipete/gogcli/internal/config"
)

func TestMCPCalendarUpdateBuildArgs(t *testing.T) {
	full := map[string]any{
		"calendar_id":           "primary",
		"event_id":              "event-1",
		"summary":               "Planning",
		"start":                 "2026-08-04T09:00:00Z",
		"end":                   "2026-08-04T10:00:00Z",
		"start_timezone":        "Europe/Rome",
		"end_timezone":          "America/New_York",
		"description":           "Weekly planning",
		"location":              "Room 1",
		"attendees":             "a@example.com,b@example.com",
		"all_day":               true,
		"rrule":                 []string{"RRULE:FREQ=WEEKLY"},
		"reminders":             []string{"popup:10", "email:30"},
		"event_color":           "7",
		"visibility":            "private",
		"transparency":          "free",
		"guests_can_invite":     true,
		"guests_can_modify":     false,
		"guests_can_see_others": true,
		"scope":                 "future",
		"original_start":        "2026-08-04T09:00:00Z",
		"send_updates":          "all",
	}
	assertMCPCalendarArgs(t, "calendar_update_event", full, []string{
		"calendar", "update",
		"--summary", "Planning",
		"--from", "2026-08-04T09:00:00Z",
		"--to", "2026-08-04T10:00:00Z",
		"--start-timezone", "Europe/Rome",
		"--end-timezone", "America/New_York",
		"--description", "Weekly planning",
		"--location", "Room 1",
		"--attendees", "a@example.com,b@example.com",
		"--all-day",
		"--rrule", "RRULE:FREQ=WEEKLY",
		"--reminder", "popup:10",
		"--reminder", "email:30",
		"--event-color", "7",
		"--visibility", "private",
		"--transparency", "free",
		"--guests-can-invite",
		"--guests-can-modify=false",
		"--guests-can-see-others",
		"--scope", "future",
		"--original-start", "2026-08-04T09:00:00Z",
		"--send-updates", "all",
		"--", "primary", "event-1",
	})

	assertMCPCalendarArgs(t, "calendar_update_event", map[string]any{
		"calendar_id": "primary",
		"event_id":    "event-1",
	}, []string{
		"calendar", "update", "--send-updates", "none", "--", "primary", "event-1",
	})

	assertMCPCalendarArgs(t, "calendar_update_event", map[string]any{
		"calendar_id":   "primary",
		"event_id":      "event-1",
		"add_attendees": "new@example.com,room@example.com",
	}, []string{
		"calendar", "update",
		"--add-attendee", "new@example.com,room@example.com",
		"--send-updates", "none",
		"--", "primary", "event-1",
	})

	assertMCPCalendarArgs(t, "calendar_update_event", map[string]any{
		"calendar_id": "primary",
		"event_id":    "event-1",
		"start":       "2026-08-04T09:00:00Z",
		"end":         "2026-08-04T10:00:00Z",
		"all_day":     false,
	}, []string{
		"calendar", "update",
		"--from", "2026-08-04T09:00:00Z",
		"--to", "2026-08-04T10:00:00Z",
		"--all-day=false",
		"--send-updates", "none",
		"--", "primary", "event-1",
	})
}

func TestMCPCalendarUpdatePresenceClearsAndDisables(t *testing.T) {
	args, err := findMCPTool(t, "calendar_update_event").BuildArgs(calendarMCPRequest(map[string]any{
		"calendar_id":           "primary",
		"event_id":              "event-1",
		"summary":               "",
		"start":                 "",
		"end":                   "",
		"description":           "",
		"location":              "",
		"attendees":             "",
		"all_day":               false,
		"rrule":                 []string{},
		"reminders":             []string{},
		"event_color":           "",
		"guests_can_invite":     false,
		"guests_can_modify":     false,
		"guests_can_see_others": false,
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"calendar", "update",
		"--summary", "",
		"--from", "",
		"--to", "",
		"--description", "",
		"--location", "",
		"--attendees", "",
		"--all-day=false",
		"--rrule=",
		"--reminder=",
		"--event-color", "",
		"--guests-can-invite=false",
		"--guests-can-modify=false",
		"--guests-can-see-others=false",
		"--send-updates", "none",
		"--", "primary", "event-1",
	}
	if got := strings.Join(args, "\x00"); got != strings.Join(want, "\x00") {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	if containsMCPArg(args, "--start-timezone") || containsMCPArg(args, "--scope") {
		t.Fatalf("absent fields emitted flags: %#v", args)
	}
}

func TestMCPCalendarUpdateRequiresNonEmptyIDs(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "missing calendar id", args: map[string]any{"event_id": "event-1"}, want: "calendar_id"},
		{name: "empty calendar id", args: map[string]any{"calendar_id": "", "event_id": "event-1"}, want: "calendar_id"},
		{name: "missing event id", args: map[string]any{"calendar_id": "primary"}, want: "event_id"},
		{name: "empty event id", args: map[string]any{"calendar_id": "primary", "event_id": ""}, want: "event_id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := findMCPTool(t, "calendar_update_event").BuildArgs(calendarMCPRequest(tt.args))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want text containing %q", err, tt.want)
			}
		})
	}
}

func TestMCPCalendarUpdateBuildArgsRejectsUnsafeCombinations(t *testing.T) {
	base := map[string]any{"calendar_id": "primary", "event_id": "event-1"}
	tests := []struct {
		name string
		add  map[string]any
		want string
	}{
		{name: "attendee replacement and addition", add: map[string]any{"attendees": "a@example.com", "add_attendees": "b@example.com"}, want: "mutually exclusive"},
		{name: "empty additive attendees", add: map[string]any{"add_attendees": ""}, want: "must not be empty"},
		{name: "all day true without start", add: map[string]any{"all_day": true, "end": "2026-08-04T10:00:00Z"}, want: "requires start and end"},
		{name: "all day false without start", add: map[string]any{"all_day": false, "end": "2026-08-04T10:00:00Z"}, want: "requires start and end"},
		{name: "start timezone without start", add: map[string]any{"start_timezone": "UTC"}, want: "start_timezone"},
		{name: "empty start timezone without start", add: map[string]any{"start_timezone": ""}, want: "start_timezone"},
		{name: "end timezone without end", add: map[string]any{"end_timezone": "UTC"}, want: "end_timezone"},
		{name: "empty end timezone without end", add: map[string]any{"end_timezone": ""}, want: "end_timezone"},
		{name: "single scope requires original start", add: map[string]any{"scope": "single"}, want: "original_start"},
		{name: "future scope requires original start", add: map[string]any{"scope": "future"}, want: "original_start"},
		{name: "invalid scope", add: map[string]any{"scope": "series"}, want: "scope"},
		{name: "empty scope", add: map[string]any{"scope": ""}, want: "scope"},
		{name: "invalid notifications", add: map[string]any{"send_updates": "notifyEveryone"}, want: "send_updates"},
		{name: "empty notifications", add: map[string]any{"send_updates": ""}, want: "send_updates"},
		{name: "wrong optional string type", add: map[string]any{"summary": 12}, want: "summary"},
		{name: "wrong optional bool type", add: map[string]any{"all_day": "true"}, want: "all_day"},
		{name: "reminders over bound", add: map[string]any{"reminders": []string{"popup:1", "popup:2", "popup:3", "popup:4", "popup:5", "popup:6"}}, want: "reminders"},
		{name: "rrule over bound", add: map[string]any{"rrule": calendarStringSlice(101, "RRULE")}, want: "rrule"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arguments := cloneCalendarArguments(base)
			for key, value := range tt.add {
				arguments[key] = value
			}
			_, err := findMCPTool(t, "calendar_update_event").BuildArgs(calendarMCPRequest(arguments))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want text containing %q", err, tt.want)
			}
		})
	}
}

func TestMCPCalendarUpdateRRuleLimitIsInclusive(t *testing.T) {
	values := calendarStringSlice(100, "RRULE")
	args, err := findMCPTool(t, "calendar_update_event").BuildArgs(calendarMCPRequest(map[string]any{
		"calendar_id": "primary",
		"event_id":    "event-1",
		"rrule":       values,
	}))
	if err != nil {
		t.Fatalf("100 rrule values: %v", err)
	}
	var got []string
	for i := range args {
		if args[i] == "--rrule" && i+1 < len(args) {
			got = append(got, args[i+1])
		}
	}
	if len(got) != len(values) {
		t.Fatalf("rrule argv values = %d, want %d", len(got), len(values))
	}
	if got[0] != values[0] || got[len(got)-1] != values[len(values)-1] {
		t.Fatalf("rrule argv endpoints = %q/%q, want %q/%q", got[0], got[len(got)-1], values[0], values[len(values)-1])
	}
}

func TestMCPCalendarUpdateSchemaIsClosedAndExcludesIntegrations(t *testing.T) {
	properties := newMCPTool(findMCPTool(t, "calendar_update_event")).InputSchema.Properties
	for _, field := range []string{
		"location_search", "location_place_id", "attachments", "with_meet", "with_zoom", "regenerate_zoom", "remove_zoom",
		"event_type", "focus_auto_decline", "ooo_auto_decline", "working_location_type", "private_props", "shared_props", "argv", "path",
	} {
		if _, exposed := properties[field]; exposed {
			t.Fatalf("excluded field %q is exposed", field)
		}
	}

	calls := map[string]int{}
	client := newCalendarMCPValidationClient(t, []string{"calendar_update_event"}, calls)
	base := map[string]any{"calendar_id": "primary", "event_id": "event-1"}
	tests := []struct {
		name string
		add  map[string]any
		want string
	}{
		{name: "unknown integration field", add: map[string]any{"with_meet": true}, want: "with_meet"},
		{name: "unknown attachment field", add: map[string]any{"attachments": []string{"https://example.test/file"}}, want: "attachments"},
		{name: "unknown generic argv field", add: map[string]any{"argv": []string{"calendar", "update"}}, want: "argv"},
		{name: "wrong start type", add: map[string]any{"start": 123}, want: "start"},
		{name: "wrong recurrence item type", add: map[string]any{"rrule": []any{"RRULE:FREQ=DAILY", 3}}, want: "rrule"},
		{name: "invalid visibility enum", add: map[string]any{"visibility": "secret"}, want: "visibility"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arguments := cloneCalendarArguments(base)
			for key, value := range tt.add {
				arguments[key] = value
			}
			before := calls["calendar_update_event"]
			result, err := client.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{
				Name:      "calendar_update_event",
				Arguments: arguments,
			}})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError || calls["calendar_update_event"] != before {
				t.Fatalf("result = %#v, calls = %d (before %d), want schema error before handler", result.Content, calls["calendar_update_event"], before)
			}
			if !strings.Contains(mcpResultText(result), tt.want) {
				t.Fatalf("result = %#v, want text containing %q", result.Content, tt.want)
			}
		})
	}

	result, err := client.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "calendar_update_event", Arguments: base,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || calls["calendar_update_event"] != 1 {
		t.Fatalf("valid result = %#v, calls = %d", result.Content, calls["calendar_update_event"])
	}
}

func TestMCPCalendarUpdateRejectsInvalidInputsBeforeChild(t *testing.T) {
	base := map[string]any{"calendar_id": "primary", "event_id": "event-1"}
	tests := []struct {
		name string
		add  map[string]any
		want string
	}{
		{name: "missing calendar id", add: map[string]any{"calendar_id": nil}, want: "calendar_id"},
		{name: "empty calendar id", add: map[string]any{"calendar_id": ""}, want: "calendar_id"},
		{name: "missing event id", add: map[string]any{"event_id": nil}, want: "event_id"},
		{name: "empty event id", add: map[string]any{"event_id": ""}, want: "event_id"},
		{name: "all day false without times", add: map[string]any{"all_day": false}, want: "all_day"},
		{name: "empty start timezone without start", add: map[string]any{"start_timezone": ""}, want: "start_timezone"},
		{name: "empty end timezone without end", add: map[string]any{"end_timezone": ""}, want: "end_timezone"},
		{name: "empty scope", add: map[string]any{"scope": ""}, want: "scope"},
		{name: "empty send updates", add: map[string]any{"send_updates": ""}, want: "send_updates"},
		{name: "rrule 101", add: map[string]any{"rrule": calendarStringSlice(101, "RRULE")}, want: "rrule"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arguments := cloneCalendarArguments(base)
			for key, value := range tt.add {
				if value == nil {
					delete(arguments, key)
					continue
				}
				arguments[key] = value
			}
			result, childCalls := callMCPCalendarUpdateBuildArgs(t, arguments)
			if !result.IsError || childCalls != 0 {
				t.Fatalf("result = %#v, child calls = %d, want pre-child error", result.Content, childCalls)
			}
			if !strings.Contains(mcpResultText(result), tt.want) {
				t.Fatalf("result = %#v, want text containing %q", result.Content, tt.want)
			}
		})
	}
}

func TestMCPEnabledToolsAllowWriteIncludesCalendarUpdate(t *testing.T) {
	if hasMCPTool(mcpEnabledTools(McpCmd{}), "calendar_update_event") {
		t.Fatal("calendar_update_event exposed without --allow-write")
	}
	if hasMCPTool(mcpEnabledTools(McpCmd{AllowTool: []string{"calendar.*"}}), "calendar_update_event") {
		t.Fatal("calendar_update_event exposed without --allow-write under calendar.*")
	}
	for _, selector := range []string{"calendar_update_event", "calendar", "calendar.*", "write", "all", "*"} {
		if !hasMCPTool(mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{selector}}), "calendar_update_event") {
			t.Fatalf("calendar_update_event missing under selector %q", selector)
		}
	}

	policy, err := normalizeMCPPolicy(config.MCPPolicy{AllowTools: []string{"calendar.*"}, AllowWrite: true})
	if err != nil {
		t.Fatal(err)
	}
	tools, err := mcpEnabledToolsWithPolicy(McpCmd{}, &RootFlags{}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMCPTool(tools, "calendar_update_event") {
		t.Fatal("calendar_update_event missing from broad persistent selector")
	}
	tools, err = mcpEnabledToolsWithPolicy(McpCmd{}, &RootFlags{ReadOnly: true}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if hasMCPTool(tools, "calendar_update_event") {
		t.Fatal("calendar_update_event exposed under readonly root")
	}
}

func TestMCPCalendarUpdateClearFieldsSerializeProviderRequest(t *testing.T) {
	var (
		patchCalls int
		patchBody  map[string]json.RawMessage
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/calendar/v3")
		if r.Method != http.MethodPatch || path != "/calendars/primary/events/event-1" {
			http.NotFound(w, r)
			return
		}
		patchCalls++
		if err := json.NewDecoder(r.Body).Decode(&patchBody); err != nil {
			t.Fatalf("decode provider patch: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"event-1"}`)
	}))
	defer srv.Close()

	svc := newCalendarServiceFromServer(t, srv)
	ctx := withCalendarTestService(newCmdRuntimeJSONOutputContext(t, io.Discard, io.Discard), svc)
	if err := runKong(t, &CalendarUpdateCmd{}, []string{
		"primary", "event-1",
		"--summary=",
		"--description=",
		"--location=",
		"--attendees=",
		"--rrule=",
		"--reminder=",
		"--event-color=",
	}, ctx, &RootFlags{Account: "a@b.com"}); err != nil {
		t.Fatalf("runKong: %v", err)
	}
	if patchCalls != 1 {
		t.Fatalf("provider patch calls = %d, want 1", patchCalls)
	}

	for _, field := range []string{"summary", "description", "location", "colorId"} {
		raw, ok := patchBody[field]
		if !ok {
			t.Fatalf("provider patch missing clear field %q: %#v", field, patchBody)
		}
		var got string
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("decode %s: %v", field, err)
		}
		if got != "" {
			t.Fatalf("provider patch %s = %q, want explicit empty string", field, got)
		}
	}

	var attendees []json.RawMessage
	if raw, ok := patchBody["attendees"]; !ok {
		t.Fatalf("provider patch missing attendees clear: %#v", patchBody)
	} else if err := json.Unmarshal(raw, &attendees); err != nil {
		t.Fatalf("decode attendees: %v", err)
	} else if attendees == nil || len(attendees) != 0 {
		t.Fatalf("provider patch attendees = %#v, want explicit empty array", attendees)
	}

	var recurrence []string
	if raw, ok := patchBody["recurrence"]; !ok {
		t.Fatalf("provider patch missing recurrence clear: %#v", patchBody)
	} else if err := json.Unmarshal(raw, &recurrence); err != nil {
		t.Fatalf("decode recurrence: %v", err)
	} else if recurrence == nil || len(recurrence) != 0 {
		t.Fatalf("provider patch recurrence = %#v, want explicit empty array", recurrence)
	}

	var reminders struct {
		UseDefault bool `json:"useDefault"`
	}
	if raw, ok := patchBody["reminders"]; !ok {
		t.Fatalf("provider patch missing reminders clear: %#v", patchBody)
	} else if err := json.Unmarshal(raw, &reminders); err != nil {
		t.Fatalf("decode reminders: %v", err)
	} else if !reminders.UseDefault {
		t.Fatalf("provider patch reminders = %#v, want useDefault=true", reminders)
	}
}

func TestMCPCalendarUpdateRunnerReturnsStructuredResult(t *testing.T) {
	t.Setenv("GOG_MCP_C07_UPDATE_RUNNER_HELPER", "1")
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self:           os.Args[0],
		tool:           findMCPTool(t, "calendar_update_event"),
		commandArgs:    []string{"-test.run=TestMCPCalendarUpdateRunnerHelper$"},
		timeout:        5 * time.Second,
		maxOutputBytes: 4096,
	})
	if result.IsError {
		t.Fatalf("runner result is error: %#v", result.Content)
	}
	got, ok := result.StructuredContent.(mcpCommandResult)
	if !ok {
		t.Fatalf("structured result type = %T, want mcpCommandResult", result.StructuredContent)
	}
	if got.Tool != "calendar_update_event" || got.Service != "calendar" || got.Risk != string(mcpRiskWrite) || got.ExitCode != 0 {
		t.Fatalf("runner metadata = %#v", got)
	}
	if got.Stderr != "calendar update fixture stderr\n" {
		t.Fatalf("stderr = %q, want separate fixture diagnostics", got.Stderr)
	}
	stdout, ok := got.Stdout.(map[string]any)
	if !ok || stdout["updated"] != true || stdout["untouched"] != "preserved" {
		t.Fatalf("structured stdout = %#v, want partial-update fixture", got.Stdout)
	}
}

func TestMCPCalendarUpdateRunnerHelper(t *testing.T) {
	if os.Getenv("GOG_MCP_C07_UPDATE_RUNNER_HELPER") != "1" {
		return
	}
	_, _ = io.WriteString(os.Stdout, `{"updated":true,"untouched":"preserved"}`)
	_, _ = io.WriteString(os.Stderr, "calendar update fixture stderr\n")
	os.Exit(0)
}

func callMCPCalendarUpdateBuildArgs(t *testing.T, arguments map[string]any) (*mcp.CallToolResult, int) {
	t.Helper()
	spec := findMCPTool(t, "calendar_update_event")
	s := newMCPServer()
	childCalls := 0
	s.AddTool(newMCPTool(spec), func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if _, err := spec.BuildArgs(req); err != nil {
			result := mcp.NewToolResultError(err.Error())
			result.IsError = true
			return result, nil
		}
		childCalls++
		return mcp.NewToolResultText("child reached"), nil
	})
	client, err := mcpclient.NewInProcessClient(s)
	if err != nil {
		t.Fatalf("new in-process MCP client: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("close MCP client: %v", closeErr)
		}
	})
	if startErr := client.Start(t.Context()); startErr != nil {
		t.Fatalf("start MCP client: %v", startErr)
	}
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "gog-calendar-update-test", Version: "1"}
	if _, initializeErr := client.Initialize(t.Context(), initRequest); initializeErr != nil {
		t.Fatalf("initialize MCP client: %v", initializeErr)
	}
	result, err := client.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name:      "calendar_update_event",
		Arguments: arguments,
	}})
	if err != nil {
		t.Fatalf("call calendar_update_event: %v", err)
	}
	return result, childCalls
}

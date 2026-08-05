package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"google.golang.org/api/calendar/v3"

	"github.com/steipete/gogcli/internal/zoom"
)

func TestMCPE05CalendarOrdinarySchemasAreClosedAndExcludeIntegrations(t *testing.T) {
	ordinaryTools := []string{"calendar_create_event", "calendar_update_event"}
	for _, name := range ordinaryTools {
		t.Run(name, func(t *testing.T) {
			spec := findMCPTool(t, name)
			tool := newMCPTool(spec)
			closed, ok := tool.InputSchema.AdditionalProperties.(bool)
			if !ok || closed {
				t.Fatalf("schema additionalProperties = %#v, want false", tool.InputSchema.AdditionalProperties)
			}
			for _, field := range mcpE05CalendarExcludedFields() {
				if _, exposed := tool.InputSchema.Properties[field]; exposed {
					t.Fatalf("ordinary event schema exposes excluded field %q", field)
				}
			}
		})
	}
}

func TestMCPE05CalendarNegativeSchemaCallsRejectExcludedFields(t *testing.T) {
	calls := map[string]int{}
	client := newCalendarMCPValidationClient(t, []string{"calendar_create_event", "calendar_update_event"}, calls)

	for _, ordinary := range []struct {
		name string
		base map[string]any
	}{
		{name: "calendar_create_event", base: calendarCreateBaseArgs()},
		{name: "calendar_update_event", base: map[string]any{"calendar_id": "primary", "event_id": "event-1"}},
	} {
		for _, excluded := range mcpE05CalendarExcludedFieldsWithValues() {
			t.Run(ordinary.name+"/"+excluded.name, func(t *testing.T) {
				arguments := cloneCalendarArguments(ordinary.base)
				arguments[excluded.name] = excluded.value
				before := calls[ordinary.name]
				result, err := client.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{
					Name:      ordinary.name,
					Arguments: arguments,
				}})
				if err != nil {
					t.Fatal(err)
				}
				if !result.IsError {
					t.Fatalf("excluded field %q was accepted: %#v", excluded.name, result.Content)
				}
				if calls[ordinary.name] != before {
					t.Fatalf("excluded field %q reached handler", excluded.name)
				}
				if !strings.Contains(mcpResultText(result), excluded.name) {
					t.Fatalf("schema error = %q, want field %q", mcpResultText(result), excluded.name)
				}
			})
		}
	}
}

func TestMCPE05CalendarOrdinaryArgvContainsNoExcludedFlags(t *testing.T) {
	createArgs, err := findMCPTool(t, "calendar_create_event").BuildArgs(calendarMCPRequest(map[string]any{
		"calendar_id":           "primary",
		"summary":               "Planning",
		"from":                  "2026-08-04T09:00:00Z",
		"to":                    "2026-08-04T10:00:00Z",
		"description":           "Weekly planning",
		"location":              "Room 1",
		"start_timezone":        "Europe/Rome",
		"end_timezone":          "America/New_York",
		"attendees":             []string{"a@example.com", "b@example.com"},
		"rrule":                 []string{"RRULE:FREQ=WEEKLY"},
		"reminders":             []string{"popup:10", "email:30"},
		"color_id":              "7",
		"visibility":            "private",
		"transparency":          "free",
		"send_updates":          "all",
		"guests_can_invite":     true,
		"guests_can_modify":     false,
		"guests_can_see_others": true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	wantCreate := []string{
		"calendar", "create", "--summary", "Planning", "--from", "2026-08-04T09:00:00Z", "--to", "2026-08-04T10:00:00Z",
		"--description", "Weekly planning", "--location", "Room 1", "--start-timezone", "Europe/Rome", "--end-timezone", "America/New_York",
		"--event-color", "7", "--visibility", "private", "--transparency", "free", "--attendees", "a@example.com,b@example.com",
		"--rrule", "RRULE:FREQ=WEEKLY", "--reminder", "popup:10", "--reminder", "email:30", "--send-updates", "all",
		"--guests-can-invite", "--no-guests-can-modify", "--guests-can-see-others", "--", "primary",
	}
	if got := strings.Join(createArgs, "\x00"); got != strings.Join(wantCreate, "\x00") {
		t.Fatalf("create argv = %#v, want %#v", createArgs, wantCreate)
	}

	updateArgs, err := findMCPTool(t, "calendar_update_event").BuildArgs(calendarMCPRequest(map[string]any{
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
	}))
	if err != nil {
		t.Fatal(err)
	}
	wantUpdate := []string{
		"calendar", "update", "--summary", "Planning", "--from", "2026-08-04T09:00:00Z", "--to", "2026-08-04T10:00:00Z",
		"--start-timezone", "Europe/Rome", "--end-timezone", "America/New_York", "--description", "Weekly planning", "--location", "Room 1",
		"--attendees", "a@example.com,b@example.com", "--rrule", "RRULE:FREQ=WEEKLY", "--reminder", "popup:10", "--reminder", "email:30",
		"--event-color", "7", "--visibility", "private", "--transparency", "free", "--guests-can-invite", "--no-guests-can-modify",
		"--guests-can-see-others", "--scope", "future", "--original-start", "2026-08-04T09:00:00Z", "--send-updates", "all", "--", "primary", "event-1",
	}
	if got := strings.Join(updateArgs, "\x00"); got != strings.Join(wantUpdate, "\x00") {
		t.Fatalf("update argv = %#v, want %#v", updateArgs, wantUpdate)
	}

	for _, flag := range mcpE05CalendarExcludedFlags() {
		if containsMCPArg(createArgs, flag) || containsMCPArg(updateArgs, flag) {
			t.Fatalf("ordinary argv emitted excluded flag %q: create=%#v update=%#v", flag, createArgs, updateArgs)
		}
	}
}

func TestMCPE05CalendarSpecializedToolsDoNotWidenOrdinarySchemas(t *testing.T) {
	specialized := []string{"calendar_focus_time", "calendar_out_of_office", "calendar_working_location"}
	for _, name := range specialized {
		if !hasMCPTool(mcpAllTools(), name) {
			t.Fatalf("dedicated specialized tool %q is missing", name)
		}
	}

	for _, selector := range []string{"calendar", "calendar.*", "write", "all", "*"} {
		t.Run(selector, func(t *testing.T) {
			tools := mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{selector}})
			for _, name := range specialized {
				if !hasMCPTool(tools, name) {
					t.Fatalf("specialized tool %q missing under selector %q: %#v", name, selector, toolNames(tools))
				}
			}
			for _, excluded := range []string{"calendar_integrations", "calendar_meet", "calendar_zoom", "calendar_places", "calendar_create_zoom_event"} {
				if hasMCPTool(tools, excluded) {
					t.Fatalf("excluded integration tool %q exposed under selector %q", excluded, selector)
				}
			}
		})
	}

	for _, name := range []string{"calendar_create_event", "calendar_update_event"} {
		properties := newMCPTool(findMCPTool(t, name)).InputSchema.Properties
		for _, field := range mcpE05CalendarSpecializedFields() {
			if _, exposed := properties[field]; exposed {
				t.Fatalf("ordinary tool %q widened by specialized fields: %q", name, field)
			}
		}
	}
}

func TestMCPE05CalendarZoomOutputRedactionFixture(t *testing.T) {
	event := &calendar.Event{
		Description: buildZoomDescriptionBlock(&zoom.Meeting{
			ID:      1001,
			JoinURL: "https://example.zoom.us/j/1001?pwd=fixture-secret&from=calendar",
		}),
		ConferenceData: &calendar.ConferenceData{
			EntryPoints: []*calendar.EntryPoint{{
				EntryPointType: "video",
				Uri:            "https://example.zoom.us/j/1001?pwd=fixture-secret",
			}},
		},
	}

	redactCalendarEventForOutput(withZoomIncludePasswords(context.Background(), false), event)
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	output := string(payload)
	if strings.Contains(output, "fixture-secret") {
		t.Fatalf("Zoom password leaked in redacted fixture: %s", output)
	}
	if !strings.Contains(output, "pwd=REDACTED") || !strings.Contains(output, "Passcode: REDACTED") {
		t.Fatalf("redacted fixture missing password markers: %s", output)
	}
}

type mcpE05CalendarExcludedField struct {
	name  string
	value any
}

func mcpE05CalendarExcludedFieldsWithValues() []mcpE05CalendarExcludedField {
	fields := make([]mcpE05CalendarExcludedField, 0, len(mcpE05CalendarExcludedFields()))
	for _, name := range mcpE05CalendarExcludedFields() {
		value := any("excluded")
		switch name {
		case "with_meet", "regenerate_meet", "with_zoom", "regenerate_zoom", "remove_zoom", "include_passwords", "include_password", "show_password", "show_passwords":
			value = true
		case "attachments", "attachment", "private_props", "private_prop", "private_properties", "shared_props", "shared_prop", "shared_properties", "extended_properties":
			value = []string{"excluded=value"}
		}
		fields = append(fields, mcpE05CalendarExcludedField{name: name, value: value})
	}
	return fields
}

func mcpE05CalendarExcludedFields() []string {
	return append(mcpE05CalendarSpecializedFields(), []string{
		"with_meet", "regenerate_meet",
		"with_zoom", "regenerate_zoom", "remove_zoom",
		"include_passwords", "include_password", "show_password", "show_passwords", "password", "passcode", "meeting_password", "zoom_password",
		"location_search", "place_id", "place_language", "place_region", "location_place_id", "places_api_key", "api_key",
		"account", "client", "credential", "credentials", "account_id", "client_id", "client_secret", "zoom_account_id", "zoom_client_id", "zoom_client_secret", "access_token", "refresh_token",
		"attachments", "attachment", "source_url", "source_title", "private_props", "private_prop", "private_properties", "shared_props", "shared_prop", "shared_properties", "extended_properties", "extended_property",
	}...)
}

func mcpE05CalendarSpecializedFields() []string {
	return []string{
		"event_type", "event_types", "type",
		"auto_decline", "decline_message", "chat_status",
		"focus_auto_decline", "focus_decline_message", "focus_chat_status", "focus_time", "focus_time_properties",
		"ooo_auto_decline", "ooo_decline_message", "out_of_office", "out_of_office_properties",
		"working_location_type", "working_office_label", "working_building_id", "working_floor_id", "working_desk_id", "working_custom_label", "working_location", "working_location_properties",
	}
}

func mcpE05CalendarExcludedFlags() []string {
	return []string{
		"--with-meet", "--regenerate-meet", "--with-zoom", "--regenerate-zoom", "--remove-zoom", "--include-passwords",
		"--location-search", "--place-id", "--place-language", "--place-region", "--attachment", "--source-url", "--source-title",
		"--private-prop", "--shared-prop", "--event-type", "--auto-decline", "--decline-message", "--chat-status",
		"--focus-auto-decline", "--focus-decline-message", "--focus-chat-status", "--ooo-auto-decline", "--ooo-decline-message",
		"--working-location-type", "--working-office-label", "--working-building-id", "--working-floor-id", "--working-desk-id", "--working-custom-label",
	}
}

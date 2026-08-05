package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestMCPWaveACalendarReadAdaptersBuildFocusedArgs(t *testing.T) {
	tests := []struct {
		name      string
		tool      string
		arguments map[string]any
		want      []string
	}{
		{
			name:      "list calendars defaults",
			tool:      "calendar_list_calendars",
			arguments: map[string]any{},
			want:      []string{"calendar", "calendars", "--max", "100"},
		},
		{
			name: "list calendars paging and all pages",
			tool: "calendar_list_calendars",
			arguments: map[string]any{
				"max":        100,
				"page_token": "page-2",
				"all_pages":  true,
			},
			want: []string{"calendar", "calendars", "--max", "100", "--page", "page-2", "--all"},
		},
		{
			name: "search all optional fields and positional separator",
			tool: "calendar_search_events",
			arguments: map[string]any{
				"query":       "--planning",
				"calendar_id": "team@example.com",
				"from":        "2026-08-04T09:00:00Z",
				"to":          "2026-08-04T17:00:00Z",
				"max":         250,
			},
			want: []string{
				"calendar", "search", "--from", "2026-08-04T09:00:00Z",
				"--to", "2026-08-04T17:00:00Z", "--calendar", "team@example.com",
				"--max", "250", "--", "--planning",
			},
		},
		{
			name: "get event timezone and positional separator",
			tool: "calendar_get_event",
			arguments: map[string]any{
				"calendar_id": "-primary",
				"event_id":    "-event",
				"timezone":    "America/New_York",
			},
			want: []string{"calendar", "event", "--timezone", "America/New_York", "--", "-primary", "-event"},
		},
		{
			name: "freebusy explicit selectors",
			tool: "calendar_freebusy",
			arguments: map[string]any{
				"calendar_id":        "primary",
				"extra_calendar_ids": []string{"team", "room"},
				"from":               "2026-08-04T09:00:00Z",
				"to":                 "2026-08-04T10:00:00Z",
			},
			want: []string{
				"calendar", "freebusy", "--cal", "team", "--cal", "room",
				"--from", "2026-08-04T09:00:00Z", "--to", "2026-08-04T10:00:00Z", "--", "primary",
			},
		},
		{
			name: "freebusy all calendars",
			tool: "calendar_freebusy",
			arguments: map[string]any{
				"all":  true,
				"from": "2026-08-04T09:00:00Z",
				"to":   "2026-08-04T10:00:00Z",
			},
			want: []string{"calendar", "freebusy", "--all", "--from", "2026-08-04T09:00:00Z", "--to", "2026-08-04T10:00:00Z"},
		},
		{
			name: "conflicts explicit calendars and range",
			tool: "calendar_find_conflicts",
			arguments: map[string]any{
				"calendar_ids": []string{"primary", "team"},
				"from":         "2026-08-04T09:00:00Z",
				"to":           "2026-08-04T17:00:00Z",
			},
			want: []string{
				"calendar", "conflicts", "--from", "2026-08-04T09:00:00Z", "--to", "2026-08-04T17:00:00Z",
				"--cal", "primary", "--cal", "team",
			},
		},
		{
			name: "conflicts all calendars and day boundary",
			tool: "calendar_find_conflicts",
			arguments: map[string]any{
				"all":  true,
				"days": 31,
			},
			want: []string{"calendar", "conflicts", "--days", "31", "--all"},
		},
		{
			name: "events M08 all supported fields",
			tool: "calendar_events",
			arguments: map[string]any{
				"calendar_id": "-primary",
				"from":        "2026-08-04T09:00:00Z",
				"to":          "2026-08-05T09:00:00Z",
				"tomorrow":    true,
				"days":        3,
				"max":         250,
				"query":       "--planning",
			},
			want: []string{
				"calendar", "events", "--from", "2026-08-04T09:00:00Z", "--to", "2026-08-05T09:00:00Z",
				"--query", "--planning", "--tomorrow", "--days", "3", "--max", "250", "--", "-primary",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertMCPCalendarArgs(t, tt.tool, tt.arguments, tt.want)
		})
	}
}

func TestMCPWaveACalendarWriteAdaptersBuildFocusedArgs(t *testing.T) {
	tests := []struct {
		name      string
		tool      string
		arguments map[string]any
		want      []string
	}{
		{
			name: "ordinary event optional fields and recurrence",
			tool: "calendar_create_event",
			arguments: map[string]any{
				"calendar_id":           "primary",
				"summary":               "Planning",
				"from":                  "2026-08-04",
				"to":                    "2026-08-05",
				"description":           "Weekly planning",
				"location":              "Room 1",
				"all_day":               true,
				"timezone":              "America/New_York",
				"attendees":             []string{"a@example.com", "b@example.com"},
				"rrule":                 []string{"RRULE:FREQ=WEEKLY"},
				"reminders":             []string{"popup:10", "email:30"},
				"color_id":              "7",
				"visibility":            "private",
				"transparency":          "transparent",
				"send_updates":          "all",
				"guests_can_invite":     true,
				"guests_can_modify":     false,
				"guests_can_see_others": true,
			},
			want: []string{
				"calendar", "create", "--summary", "Planning", "--from", "2026-08-04", "--to", "2026-08-05",
				"--description", "Weekly planning", "--location", "Room 1", "--timezone", "America/New_York",
				"--event-color", "7", "--visibility", "private", "--transparency", "transparent", "--all-day",
				"--attendees", "a@example.com,b@example.com", "--rrule", "RRULE:FREQ=WEEKLY", "--reminder", "popup:10",
				"--reminder", "email:30", "--send-updates", "all", "--guests-can-invite", "--no-guests-can-modify",
				"--guests-can-see-others", "--", "primary",
			},
		},
		{
			name: "respond with optional comment",
			tool: "calendar_respond_to_event",
			arguments: map[string]any{
				"calendar_id": "primary",
				"event_id":    "event-1",
				"status":      "tentative",
				"comment":     "Maybe",
			},
			want: []string{"calendar", "respond", "--status", "tentative", "--comment", "Maybe", "--", "primary", "event-1"},
		},
		{
			name: "move with explicit notifications",
			tool: "calendar_move_event",
			arguments: map[string]any{
				"source_calendar_id":      "source",
				"event_id":                "event-1",
				"destination_calendar_id": "destination",
				"send_updates":            "externalOnly",
			},
			want: []string{"calendar", "move", "--send-updates", "externalOnly", "--", "source", "event-1", "destination"},
		},
		{
			name: "create secondary calendar optional fields",
			tool: "calendar_create_calendar",
			arguments: map[string]any{
				"summary":     "Team",
				"description": "Team calendar",
				"timezone":    "Europe/London",
				"location":    "London",
			},
			want: []string{"calendar", "create-calendar", "--description", "Team calendar", "--timezone", "Europe/London", "--location", "London", "--", "Team"},
		},
		{
			name: "subscribe raw ID and all subscription controls",
			tool: "calendar_subscribe",
			arguments: map[string]any{
				"calendar_id": "raw@example.com",
				"color_id":    "24",
				"hidden":      true,
				"selected":    false,
			},
			want: []string{"calendar", "subscribe", "--color-id", "24", "--hidden", "--no-selected", "--", "raw@example.com"},
		},
		{
			name:      "unsubscribe is forced and positional",
			tool:      "calendar_unsubscribe",
			arguments: map[string]any{"calendar_id": "team@example.com"},
			want:      []string{"calendar", "unsubscribe", "--force", "--", "team@example.com"},
		},
		{
			name: "focus time defaults overridden with recurrence",
			tool: "calendar_focus_time",
			arguments: map[string]any{
				"from":            "2026-08-04T09:00:00Z",
				"to":              "2026-08-04T10:00:00Z",
				"calendar_id":     "focus@example.com",
				"summary":         "Deep work",
				"auto_decline":    "new",
				"decline_message": "Please do not book over focus time",
				"chat_status":     "available",
				"rrule":           []string{"RRULE:FREQ=DAILY", "RRULE:COUNT=3"},
			},
			want: []string{
				"calendar", "focus-time", "--summary", "Deep work", "--from", "2026-08-04T09:00:00Z",
				"--to", "2026-08-04T10:00:00Z", "--auto-decline", "new", "--chat-status", "available",
				"--decline-message", "Please do not book over focus time", "--rrule", "RRULE:FREQ=DAILY",
				"--rrule", "RRULE:COUNT=3", "--", "focus@example.com",
			},
		},
		{
			name: "out of office defaults overridden",
			tool: "calendar_out_of_office",
			arguments: map[string]any{
				"from":            "2026-08-04T09:00:00Z",
				"to":              "2026-08-05T09:00:00Z",
				"calendar_id":     "ooo@example.com",
				"summary":         "Vacation",
				"auto_decline":    "none",
				"decline_message": "Away",
			},
			want: []string{
				"calendar", "out-of-office", "--summary", "Vacation", "--from", "2026-08-04T09:00:00Z",
				"--to", "2026-08-05T09:00:00Z", "--auto-decline", "none", "--decline-message", "Away",
				"--", "ooo@example.com",
			},
		},
		{
			name: "office working location optional office fields",
			tool: "calendar_working_location",
			arguments: map[string]any{
				"from":         "2026-08-04",
				"to":           "2026-08-05",
				"type":         "office",
				"calendar_id":  "work@example.com",
				"office_label": "HQ",
				"building_id":  "B1",
				"floor_id":     "4",
				"desk_id":      "D42",
			},
			want: []string{
				"calendar", "working-location", "--type", "office", "--from", "2026-08-04", "--to", "2026-08-05",
				"--office-label", "HQ", "--building-id", "B1", "--floor-id", "4", "--desk-id", "D42", "--", "work@example.com",
			},
		},
		{
			name: "custom working location",
			tool: "calendar_working_location",
			arguments: map[string]any{
				"from":         "2026-08-04",
				"to":           "2026-08-05",
				"type":         "custom",
				"custom_label": "Client site",
			},
			want: []string{
				"calendar", "working-location", "--type", "custom", "--from", "2026-08-04", "--to", "2026-08-05",
				"--custom-label", "Client site", "--", "primary",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertMCPCalendarArgs(t, tt.tool, tt.arguments, tt.want)
		})
	}
}

func TestMCPWaveACalendarNotificationDefaultsAreExplicit(t *testing.T) {
	tests := []struct {
		name      string
		tool      string
		arguments map[string]any
		want      []string
	}{
		{
			name: "ordinary event defaults to no notifications",
			tool: "calendar_create_event",
			arguments: map[string]any{
				"calendar_id": "primary",
				"summary":     "Plan",
				"from":        "2026-08-04T09:00:00Z",
				"to":          "2026-08-04T10:00:00Z",
			},
			want: []string{
				"calendar", "create", "--summary", "Plan", "--from", "2026-08-04T09:00:00Z", "--to", "2026-08-04T10:00:00Z",
				"--send-updates", "none", "--", "primary",
			},
		},
		{
			name: "ordinary event explicit notification mode",
			tool: "calendar_create_event",
			arguments: map[string]any{
				"calendar_id":  "primary",
				"summary":      "Plan",
				"from":         "2026-08-04T09:00:00Z",
				"to":           "2026-08-04T10:00:00Z",
				"send_updates": "externalOnly",
			},
			want: []string{
				"calendar", "create", "--summary", "Plan", "--from", "2026-08-04T09:00:00Z", "--to", "2026-08-04T10:00:00Z",
				"--send-updates", "externalOnly", "--", "primary",
			},
		},
		{
			name: "move defaults to no notifications",
			tool: "calendar_move_event",
			arguments: map[string]any{
				"source_calendar_id":      "source",
				"event_id":                "event-1",
				"destination_calendar_id": "destination",
			},
			want: []string{"calendar", "move", "--send-updates", "none", "--", "source", "event-1", "destination"},
		},
		{
			name: "move explicit notification mode",
			tool: "calendar_move_event",
			arguments: map[string]any{
				"source_calendar_id":      "source",
				"event_id":                "event-1",
				"destination_calendar_id": "destination",
				"send_updates":            "all",
			},
			want: []string{"calendar", "move", "--send-updates", "all", "--", "source", "event-1", "destination"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertMCPCalendarArgs(t, tt.tool, tt.arguments, tt.want)
		})
	}

	respondProperties := newMCPTool(findMCPTool(t, "calendar_respond_to_event")).InputSchema.Properties
	if _, exposed := respondProperties["send_updates"]; exposed {
		t.Fatal("calendar_respond_to_event must not expose notification controls")
	}
}

func TestMCPWaveACalendarGuestBooleanPresence(t *testing.T) {
	base := map[string]any{
		"calendar_id": "primary",
		"summary":     "Plan",
		"from":        "2026-08-04T09:00:00Z",
		"to":          "2026-08-04T10:00:00Z",
	}
	minimal, err := findMCPTool(t, "calendar_create_event").BuildArgs(calendarMCPRequest(base))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(minimal, "\x00"), "guests-") {
		t.Fatalf("absent guest booleans emitted flags: %#v", minimal)
	}

	for _, test := range []struct {
		field string
		value bool
		flag  string
	}{
		{field: "guests_can_invite", value: true, flag: "--guests-can-invite"},
		{field: "guests_can_invite", value: false, flag: "--no-guests-can-invite"},
		{field: "guests_can_modify", value: true, flag: "--guests-can-modify"},
		{field: "guests_can_modify", value: false, flag: "--no-guests-can-modify"},
		{field: "guests_can_see_others", value: true, flag: "--guests-can-see-others"},
		{field: "guests_can_see_others", value: false, flag: "--no-guests-can-see-others"},
	} {
		t.Run(test.field+"/"+test.flag, func(t *testing.T) {
			arguments := cloneCalendarArguments(base)
			arguments[test.field] = test.value
			args, err := findMCPTool(t, "calendar_create_event").BuildArgs(calendarMCPRequest(arguments))
			if err != nil {
				t.Fatal(err)
			}
			if !containsMCPArg(args, test.flag) {
				t.Fatalf("args = %#v, want %q", args, test.flag)
			}
		})
	}
}

func TestMCPWaveACalendarSchemasRejectInvalidCallsBeforeHandler(t *testing.T) {
	toolNames := []string{
		"calendar_events",
		"calendar_list_calendars",
		"calendar_search_events",
		"calendar_get_event",
		"calendar_freebusy",
		"calendar_find_conflicts",
		"calendar_create_event",
		"calendar_respond_to_event",
		"calendar_move_event",
		"calendar_create_calendar",
		"calendar_subscribe",
		"calendar_unsubscribe",
		"calendar_focus_time",
		"calendar_out_of_office",
		"calendar_working_location",
	}
	calls := make(map[string]int, len(toolNames))
	client := newCalendarMCPValidationClient(t, toolNames, calls)

	createBase := calendarCreateBaseArgs()
	tests := []struct {
		name      string
		tool      string
		arguments map[string]any
		wantText  string
	}{
		{name: "events unknown page field", tool: "calendar_events", arguments: map[string]any{"page_token": "next"}, wantText: "page_token"},
		{name: "events wrong max type", tool: "calendar_events", arguments: map[string]any{"max": "10"}, wantText: "max"},
		{name: "list wrong max type", tool: "calendar_list_calendars", arguments: map[string]any{"max": "100"}, wantText: "max"},
		{name: "list unknown page field", tool: "calendar_list_calendars", arguments: map[string]any{"page": "next"}, wantText: "page"},
		{name: "search missing query", tool: "calendar_search_events", arguments: map[string]any{}, wantText: "query"},
		{name: "search wrong query type", tool: "calendar_search_events", arguments: map[string]any{"query": 12}, wantText: "query"},
		{name: "get missing event ID", tool: "calendar_get_event", arguments: map[string]any{"calendar_id": "primary"}, wantText: "event_id"},
		{name: "get wrong calendar ID type", tool: "calendar_get_event", arguments: map[string]any{"calendar_id": 12, "event_id": "event-1"}, wantText: "calendar_id"},
		{name: "freebusy missing from", tool: "calendar_freebusy", arguments: map[string]any{"to": "2026-08-04T10:00:00Z"}, wantText: "from"},
		{name: "freebusy wrong extra selector type", tool: "calendar_freebusy", arguments: map[string]any{"from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z", "extra_calendar_ids": "team"}, wantText: "extra_calendar_ids"},
		{name: "conflicts wrong calendar IDs type", tool: "calendar_find_conflicts", arguments: map[string]any{"calendar_ids": "primary", "days": 1}, wantText: "calendar_ids"},
		{name: "conflicts wrong days type", tool: "calendar_find_conflicts", arguments: map[string]any{"days": "1", "all": true}, wantText: "days"},
		{name: "create missing calendar ID", tool: "calendar_create_event", arguments: map[string]any{"summary": "Plan", "from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z"}, wantText: "calendar_id"},
		{name: "create wrong attendee item type", tool: "calendar_create_event", arguments: map[string]any{"calendar_id": "primary", "summary": "Plan", "from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z", "attendees": []any{"a@example.com", 3}}, wantText: "attendees"},
		{name: "create rejects Meet field", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"with_meet": true}), wantText: "with_meet"},
		{name: "create rejects Zoom field", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"with_zoom": true}), wantText: "with_zoom"},
		{name: "create rejects Zoom password field", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"include_passwords": true}), wantText: "include_passwords"},
		{name: "create rejects Places search field", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"location_search": "office"}), wantText: "location_search"},
		{name: "create rejects Places ID field", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"place_id": "place-1"}), wantText: "place_id"},
		{name: "create rejects Places language field", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"place_language": "en"}), wantText: "place_language"},
		{name: "create rejects Places region field", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"place_region": "us"}), wantText: "place_region"},
		{name: "create rejects attachment field", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"attachments": []string{"https://example.com/file"}}), wantText: "attachments"},
		{name: "create rejects source URL field", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"source_url": "https://example.com"}), wantText: "source_url"},
		{name: "create rejects source title field", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"source_title": "Import"}), wantText: "source_title"},
		{name: "create rejects private extended properties", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"private_props": []string{"k=v"}}), wantText: "private_props"},
		{name: "create rejects shared extended properties", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"shared_props": []string{"k=v"}}), wantText: "shared_props"},
		{name: "create rejects event type field", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"event_type": "focus-time"}), wantText: "event_type"},
		{name: "create rejects Focus Time auto-decline field", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"focus_auto_decline": "all"}), wantText: "focus_auto_decline"},
		{name: "create rejects Focus Time decline-message field", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"focus_decline_message": "Away"}), wantText: "focus_decline_message"},
		{name: "create rejects Focus Time chat-status field", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"focus_chat_status": "available"}), wantText: "focus_chat_status"},
		{name: "create rejects OOO auto-decline field", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"ooo_auto_decline": "all"}), wantText: "ooo_auto_decline"},
		{name: "create rejects OOO decline-message field", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"ooo_decline_message": "Away"}), wantText: "ooo_decline_message"},
		{name: "create rejects working-location type field", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"working_location_type": "office"}), wantText: "working_location_type"},
		{name: "create rejects working-location office label", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"working_office_label": "HQ"}), wantText: "working_office_label"},
		{name: "create rejects working-location building ID", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"working_building_id": "B1"}), wantText: "working_building_id"},
		{name: "create rejects working-location floor ID", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"working_floor_id": "4"}), wantText: "working_floor_id"},
		{name: "create rejects working-location desk ID", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"working_desk_id": "D42"}), wantText: "working_desk_id"},
		{name: "create rejects working-location custom label", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"working_custom_label": "Client"}), wantText: "working_custom_label"},
		{name: "create invalid visibility enum", tool: "calendar_create_event", arguments: mergeCalendarArguments(createBase, map[string]any{"visibility": "secret"}), wantText: "visibility"},
		{name: "respond missing status", tool: "calendar_respond_to_event", arguments: map[string]any{"calendar_id": "primary", "event_id": "event-1"}, wantText: "status"},
		{name: "respond invalid status enum", tool: "calendar_respond_to_event", arguments: map[string]any{"calendar_id": "primary", "event_id": "event-1", "status": "maybe"}, wantText: "status"},
		{name: "move missing destination", tool: "calendar_move_event", arguments: map[string]any{"source_calendar_id": "source", "event_id": "event-1"}, wantText: "destination_calendar_id"},
		{name: "move invalid notification enum", tool: "calendar_move_event", arguments: map[string]any{"source_calendar_id": "source", "event_id": "event-1", "destination_calendar_id": "destination", "send_updates": "notifyEveryone"}, wantText: "send_updates"},
		{name: "create calendar missing summary", tool: "calendar_create_calendar", arguments: map[string]any{}, wantText: "summary"},
		{name: "create calendar wrong timezone type", tool: "calendar_create_calendar", arguments: map[string]any{"summary": "Team", "timezone": 5}, wantText: "timezone"},
		{name: "subscribe missing calendar ID", tool: "calendar_subscribe", arguments: map[string]any{}, wantText: "calendar_id"},
		{name: "subscribe wrong hidden type", tool: "calendar_subscribe", arguments: map[string]any{"calendar_id": "raw@example.com", "hidden": "true"}, wantText: "hidden"},
		{name: "unsubscribe missing calendar ID", tool: "calendar_unsubscribe", arguments: map[string]any{}, wantText: "calendar_id"},
		{name: "unsubscribe wrong ID type", tool: "calendar_unsubscribe", arguments: map[string]any{"calendar_id": false}, wantText: "calendar_id"},
		{name: "focus missing from", tool: "calendar_focus_time", arguments: map[string]any{"to": "2026-08-04T10:00:00Z"}, wantText: "from"},
		{name: "focus invalid auto decline enum", tool: "calendar_focus_time", arguments: map[string]any{"from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z", "auto_decline": "everyone"}, wantText: "auto_decline"},
		{name: "focus wrong recurrence item type", tool: "calendar_focus_time", arguments: map[string]any{"from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z", "rrule": []any{"RRULE:FREQ=DAILY", 1}}, wantText: "rrule"},
		{name: "out of office missing to", tool: "calendar_out_of_office", arguments: map[string]any{"from": "2026-08-04T09:00:00Z"}, wantText: "to"},
		{name: "out of office invalid auto decline enum", tool: "calendar_out_of_office", arguments: map[string]any{"from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z", "auto_decline": "everyone"}, wantText: "auto_decline"},
		{name: "out of office wrong decline message type", tool: "calendar_out_of_office", arguments: map[string]any{"from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z", "decline_message": 4}, wantText: "decline_message"},
		{name: "working location missing type", tool: "calendar_working_location", arguments: map[string]any{"from": "2026-08-04", "to": "2026-08-05"}, wantText: "type"},
		{name: "working location invalid type enum", tool: "calendar_working_location", arguments: map[string]any{"from": "2026-08-04", "to": "2026-08-05", "type": "somewhere"}, wantText: "type"},
		{name: "working location wrong from type", tool: "calendar_working_location", arguments: map[string]any{"from": 20260804, "to": "2026-08-05", "type": "home"}, wantText: "from"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := calls[tt.tool]
			result, err := client.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: tt.tool, Arguments: tt.arguments}})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError {
				t.Fatalf("result = %#v, want schema error", result.Content)
			}
			if !strings.Contains(mcpResultText(result), tt.wantText) {
				t.Fatalf("result = %#v, want text containing %q", result.Content, tt.wantText)
			}
			if calls[tt.tool] != before {
				t.Fatalf("invalid %s call reached handler", tt.tool)
			}
		})
	}
	for _, name := range toolNames {
		if calls[name] != 0 {
			t.Fatalf("invalid calls reached %s handler %d times", name, calls[name])
		}
	}
}

func TestMCPWaveACalendarBuildRejectsDomainInvalidInputs(t *testing.T) {
	base := calendarCreateBaseArgs()
	tests := []struct {
		name      string
		tool      string
		arguments map[string]any
		wantText  string
	}{
		{name: "freebusy all with explicit selector", tool: "calendar_freebusy", arguments: map[string]any{"all": true, "calendar_id": "primary", "from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z"}, wantText: "all cannot"},
		{name: "freebusy all with extra selectors", tool: "calendar_freebusy", arguments: map[string]any{"all": true, "extra_calendar_ids": []string{"team"}, "from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z"}, wantText: "all cannot"},
		{name: "conflicts missing range and days", tool: "calendar_find_conflicts", arguments: map[string]any{"calendar_ids": []string{"primary", "team"}}, wantText: "provide from and to"},
		{name: "conflicts only one range bound", tool: "calendar_find_conflicts", arguments: map[string]any{"from": "2026-08-04T09:00:00Z", "calendar_ids": []string{"primary", "team"}}, wantText: "provide from and to"},
		{name: "conflicts fewer than two calendars", tool: "calendar_find_conflicts", arguments: map[string]any{"days": 1, "calendar_ids": []string{"primary"}}, wantText: "at least two"},
		{name: "conflicts all with explicit calendars", tool: "calendar_find_conflicts", arguments: map[string]any{"days": 1, "all": true, "calendar_ids": []string{"primary", "team"}}, wantText: "all cannot"},
		{name: "create timezone conflict", tool: "calendar_create_event", arguments: mergeCalendarArguments(base, map[string]any{"timezone": "UTC", "start_timezone": "America/New_York"}), wantText: "timezone cannot"},
		{name: "create all day requires dates", tool: "calendar_create_event", arguments: mergeCalendarArguments(base, map[string]any{"all_day": true}), wantText: "all_day requires"},
		{name: "create invalid datetime", tool: "calendar_create_event", arguments: mergeCalendarArguments(base, map[string]any{"from": "tomorrow"}), wantText: "from must be RFC3339"},
		{name: "create invalid timezone", tool: "calendar_create_event", arguments: mergeCalendarArguments(base, map[string]any{"timezone": "Not/AZone"}), wantText: "IANA timezone"},
		{name: "create invalid notification enum", tool: "calendar_create_event", arguments: mergeCalendarArguments(base, map[string]any{"send_updates": "notifyEveryone"}), wantText: "send_updates"},
		{name: "create unsafe summary", tool: "calendar_create_event", arguments: mergeCalendarArguments(base, map[string]any{"summary": "-not-a-value"}), wantText: "unsafe"},
		{name: "create unsafe description", tool: "calendar_create_event", arguments: mergeCalendarArguments(base, map[string]any{"description": "line one\nline two"}), wantText: "unsafe"},
		{name: "create too many reminders", tool: "calendar_create_event", arguments: mergeCalendarArguments(base, map[string]any{"reminders": []string{"popup:1", "popup:2", "popup:3", "popup:4", "popup:5", "popup:6"}}), wantText: "at most 5"},
		{name: "move same calendar", tool: "calendar_move_event", arguments: map[string]any{"source_calendar_id": "Primary", "event_id": "event-1", "destination_calendar_id": "primary"}, wantText: "must differ"},
		{name: "subscribe color below range", tool: "calendar_subscribe", arguments: map[string]any{"calendar_id": "raw@example.com", "color_id": "0"}, wantText: "1 through 24"},
		{name: "subscribe color above range", tool: "calendar_subscribe", arguments: map[string]any{"calendar_id": "raw@example.com", "color_id": "25"}, wantText: "1 through 24"},
		{name: "focus invalid datetime", tool: "calendar_focus_time", arguments: map[string]any{"from": "tomorrow", "to": "2026-08-04T10:00:00Z"}, wantText: "RFC3339"},
		{name: "focus empty calendar ID", tool: "calendar_focus_time", arguments: map[string]any{"from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z", "calendar_id": " "}, wantText: "must not be empty"},
		{name: "focus invalid chat status", tool: "calendar_focus_time", arguments: map[string]any{"from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z", "chat_status": "busy"}, wantText: "chat_status"},
		{name: "out of office invalid datetime", tool: "calendar_out_of_office", arguments: map[string]any{"from": "2026-08-04", "to": "2026-08-05"}, wantText: "RFC3339"},
		{name: "out of office empty message", tool: "calendar_out_of_office", arguments: map[string]any{"from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z", "decline_message": " "}, wantText: "must not be empty"},
		{name: "working location requires date-only bounds", tool: "calendar_working_location", arguments: map[string]any{"from": "2026-08-04T09:00:00Z", "to": "2026-08-05", "type": "home"}, wantText: "YYYY-MM-DD"},
		{name: "home rejects office fields", tool: "calendar_working_location", arguments: map[string]any{"from": "2026-08-04", "to": "2026-08-05", "type": "home", "building_id": "B1"}, wantText: "does not accept office"},
		{name: "office rejects custom label", tool: "calendar_working_location", arguments: map[string]any{"from": "2026-08-04", "to": "2026-08-05", "type": "office", "custom_label": "Client site"}, wantText: "does not accept custom_label"},
		{name: "custom requires label", tool: "calendar_working_location", arguments: map[string]any{"from": "2026-08-04", "to": "2026-08-05", "type": "custom"}, wantText: "custom_label is required"},
		{name: "custom rejects office fields", tool: "calendar_working_location", arguments: map[string]any{"from": "2026-08-04", "to": "2026-08-05", "type": "custom", "custom_label": "Client site", "desk_id": "D42"}, wantText: "does not accept office"},
		{name: "working location empty calendar ID", tool: "calendar_working_location", arguments: map[string]any{"from": "2026-08-04", "to": "2026-08-05", "type": "home", "calendar_id": " "}, wantText: "must not be empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := findMCPTool(t, tt.tool).BuildArgs(calendarMCPRequest(tt.arguments))
			if err == nil || !strings.Contains(err.Error(), tt.wantText) {
				t.Fatalf("error = %v, want text containing %q", err, tt.wantText)
			}
		})
	}
}

func TestMCPWaveACalendarArrayBoundariesAndRecurringSeparation(t *testing.T) {
	oneHundred := calendarStringSlice(100, "calendar")
	oneHundredOne := calendarStringSlice(101, "calendar")

	freebusyBase := map[string]any{
		"from": "2026-08-04T09:00:00Z",
		"to":   "2026-08-04T10:00:00Z",
	}
	if _, err := findMCPTool(t, "calendar_freebusy").BuildArgs(calendarMCPRequest(mergeCalendarArguments(freebusyBase, map[string]any{"extra_calendar_ids": oneHundred}))); err != nil {
		t.Fatalf("100 freebusy selectors: %v", err)
	}
	if _, err := findMCPTool(t, "calendar_freebusy").BuildArgs(calendarMCPRequest(mergeCalendarArguments(freebusyBase, map[string]any{"extra_calendar_ids": oneHundredOne}))); err == nil {
		t.Fatal("expected 101 freebusy selectors to be rejected")
	}

	conflictsBase := map[string]any{
		"from": "2026-08-04T09:00:00Z",
		"to":   "2026-08-04T10:00:00Z",
	}
	if _, err := findMCPTool(t, "calendar_find_conflicts").BuildArgs(calendarMCPRequest(mergeCalendarArguments(conflictsBase, map[string]any{"calendar_ids": oneHundred}))); err != nil {
		t.Fatalf("100 conflict selectors: %v", err)
	}
	if _, err := findMCPTool(t, "calendar_find_conflicts").BuildArgs(calendarMCPRequest(mergeCalendarArguments(conflictsBase, map[string]any{"calendar_ids": oneHundredOne}))); err == nil {
		t.Fatal("expected 101 conflict selectors to be rejected")
	}

	createBase := calendarCreateBaseArgs()
	for _, field := range []string{"attendees", "rrule"} {
		arguments := mergeCalendarArguments(createBase, map[string]any{field: oneHundred})
		if _, err := findMCPTool(t, "calendar_create_event").BuildArgs(calendarMCPRequest(arguments)); err != nil {
			t.Fatalf("100 %s: %v", field, err)
		}
		arguments = mergeCalendarArguments(createBase, map[string]any{field: oneHundredOne})
		if _, err := findMCPTool(t, "calendar_create_event").BuildArgs(calendarMCPRequest(arguments)); err == nil {
			t.Fatalf("expected 101 %s to be rejected", field)
		}
	}

	focusBase := map[string]any{
		"from": "2026-08-04T09:00:00Z",
		"to":   "2026-08-04T10:00:00Z",
	}
	if _, err := findMCPTool(t, "calendar_focus_time").BuildArgs(calendarMCPRequest(mergeCalendarArguments(focusBase, map[string]any{"rrule": oneHundred}))); err != nil {
		t.Fatalf("100 focus recurrence rules: %v", err)
	}
	if _, err := findMCPTool(t, "calendar_focus_time").BuildArgs(calendarMCPRequest(mergeCalendarArguments(focusBase, map[string]any{"rrule": oneHundredOne}))); err == nil {
		t.Fatal("expected 101 focus recurrence rules to be rejected")
	}

	fiveReminders := []string{"popup:1", "popup:2", "popup:3", "popup:4", "popup:5"}
	if _, err := findMCPTool(t, "calendar_create_event").BuildArgs(calendarMCPRequest(mergeCalendarArguments(createBase, map[string]any{"reminders": fiveReminders}))); err != nil {
		t.Fatalf("five reminders: %v", err)
	}

	createProperties := newMCPTool(findMCPTool(t, "calendar_create_event")).InputSchema.Properties
	if _, present := createProperties["rrule"]; !present {
		t.Fatal("ordinary events must expose recurrence rules")
	}
	for _, field := range []string{"event_type", "event_types", "focus_time", "out_of_office", "working_location"} {
		if _, present := createProperties[field]; present {
			t.Fatalf("ordinary event exposes special-event field %q", field)
		}
	}
	focusProperties := newMCPTool(findMCPTool(t, "calendar_focus_time")).InputSchema.Properties
	if _, present := focusProperties["rrule"]; !present {
		t.Fatal("focus time must expose recurrence rules")
	}
	for _, toolName := range []string{"calendar_out_of_office", "calendar_working_location"} {
		properties := newMCPTool(findMCPTool(t, toolName)).InputSchema.Properties
		if _, present := properties["rrule"]; present {
			t.Fatalf("%s must not expose recurrence rules", toolName)
		}
	}
}

func TestMCPWaveACalendarSchemasAreClosedAndExcludeDeferredSurfaces(t *testing.T) {
	calendarTools := []string{
		"calendar_events",
		"calendar_list_calendars",
		"calendar_search_events",
		"calendar_get_event",
		"calendar_freebusy",
		"calendar_find_conflicts",
		"calendar_create_event",
		"calendar_respond_to_event",
		"calendar_move_event",
		"calendar_create_calendar",
		"calendar_subscribe",
		"calendar_unsubscribe",
		"calendar_focus_time",
		"calendar_out_of_office",
		"calendar_working_location",
	}
	for _, name := range calendarTools {
		t.Run(name, func(t *testing.T) {
			spec := findMCPTool(t, name)
			tool := newMCPTool(spec)
			closed, ok := tool.InputSchema.AdditionalProperties.(bool)
			if !ok || closed {
				t.Fatalf("schema additionalProperties = %#v, want false", tool.InputSchema.AdditionalProperties)
			}
			for _, field := range []string{"update", "delete", "integration", "integrations", "local_path", "path", "stdin", "argv", "output_path"} {
				if _, exposed := tool.InputSchema.Properties[field]; exposed {
					t.Fatalf("deferred or unsafe field %q exposed", field)
				}
			}
		})
	}
	for _, forbidden := range []string{"calendar_update_event", "calendar_delete_event", "calendar_delete_calendar", "calendar_integrations", "calendar_acl"} {
		if hasMCPTool(mcpAllTools(), forbidden) {
			t.Fatalf("deferred Calendar tool %q exposed", forbidden)
		}
	}

	eventsProperties := newMCPTool(findMCPTool(t, "calendar_events")).InputSchema.Properties
	for _, field := range []string{"calendars", "page_token", "all_pages", "event_types", "event_type", "weekday", "private_prop_filter", "shared_prop_filter"} {
		if _, exposed := eventsProperties[field]; exposed {
			t.Fatalf("M08 events exposes deferred or unscoped field %q", field)
		}
	}
}

func TestMCPWaveACalendarSubscriptionSemantics(t *testing.T) {
	subscribe := newMCPTool(findMCPTool(t, "calendar_subscribe")).InputSchema.Properties
	for _, field := range []string{"calendar_id", "color_id", "hidden", "selected"} {
		if _, present := subscribe[field]; !present {
			t.Fatalf("subscribe missing %q", field)
		}
	}
	for _, field := range []string{"selector", "calendar_name", "calendar_index", "all", "calendars"} {
		if _, present := subscribe[field]; present {
			t.Fatalf("subscribe exposes selector field %q; subscription requires a raw ID", field)
		}
	}
	unsubscribe := newMCPTool(findMCPTool(t, "calendar_unsubscribe")).InputSchema.Properties
	if _, present := unsubscribe["force"]; present {
		t.Fatal("unsubscribe must keep --force server-controlled")
	}

	for _, test := range []struct {
		name      string
		arguments map[string]any
		want      []string
	}{
		{name: "selected defaults true", arguments: map[string]any{"calendar_id": "raw@example.com"}, want: []string{"calendar", "subscribe", "--", "raw@example.com"}},
		{name: "explicit selected true remains selected", arguments: map[string]any{"calendar_id": "raw@example.com", "selected": true}, want: []string{"calendar", "subscribe", "--", "raw@example.com"}},
		{name: "explicit selected false disables selection", arguments: map[string]any{"calendar_id": "raw@example.com", "selected": false}, want: []string{"calendar", "subscribe", "--no-selected", "--", "raw@example.com"}},
		{name: "hidden only", arguments: map[string]any{"calendar_id": "raw@example.com", "hidden": true}, want: []string{"calendar", "subscribe", "--hidden", "--", "raw@example.com"}},
		{name: "raw ID that resembles a selector", arguments: map[string]any{"calendar_id": "primary"}, want: []string{"calendar", "subscribe", "--", "primary"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertMCPCalendarArgs(t, "calendar_subscribe", test.arguments, test.want)
		})
	}
}

func TestMCPWaveACalendarSelectorAndReadOnlyPolicy(t *testing.T) {
	readTools := []string{
		"calendar_events",
		"calendar_list_calendars",
		"calendar_search_events",
		"calendar_get_event",
		"calendar_freebusy",
		"calendar_find_conflicts",
	}
	writeTools := []string{
		"calendar_create_event",
		"calendar_respond_to_event",
		"calendar_move_event",
		"calendar_create_calendar",
		"calendar_subscribe",
		"calendar_unsubscribe",
		"calendar_focus_time",
		"calendar_out_of_office",
		"calendar_working_location",
	}
	for _, name := range readTools {
		if !hasMCPTool(mcpEnabledTools(McpCmd{}), name) {
			t.Errorf("read tool %q missing from default policy", name)
		}
		if !hasMCPTool(mcpEnabledTools(McpCmd{AllowTool: []string{"calendar"}}), name) {
			t.Errorf("read tool %q missing from calendar selector", name)
		}
		if !hasMCPTool(mcpEnabledTools(McpCmd{AllowTool: []string{"calendar.*"}}), name) {
			t.Errorf("read tool %q missing from calendar wildcard selector", name)
		}
	}
	for _, name := range writeTools {
		if hasMCPTool(mcpEnabledTools(McpCmd{}), name) {
			t.Errorf("write tool %q exposed by default policy", name)
		}
		if hasMCPTool(mcpEnabledTools(McpCmd{AllowTool: []string{"calendar"}}), name) {
			t.Errorf("write tool %q exposed without --allow-write", name)
		}
		for _, selector := range []string{name, "calendar", "calendar.*", "write", "all"} {
			if !hasMCPTool(mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{selector}}), name) {
				t.Errorf("write tool %q missing from authorized selector %q", name, selector)
			}
		}
	}
	for _, name := range append(readTools, writeTools...) {
		spec := findMCPTool(t, name)
		wantRisk := mcpRiskRead
		if containsMCPName(writeTools, name) {
			wantRisk = mcpRiskWrite
		}
		if spec.Risk != wantRisk || spec.Service != "calendar" {
			t.Errorf("%s policy metadata = service %q risk %q, want calendar/%q", name, spec.Service, spec.Risk, wantRisk)
		}
	}
}

func assertMCPCalendarArgs(t *testing.T, toolName string, arguments map[string]any, want []string) {
	t.Helper()
	args, err := findMCPTool(t, toolName).BuildArgs(calendarMCPRequest(arguments))
	if err != nil {
		t.Fatalf("BuildArgs: %v", err)
	}
	if got := strings.Join(args, "\x00"); got != strings.Join(want, "\x00") {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	for _, arg := range args {
		if arg == "--json" {
			t.Fatalf("adapter supplied runner-owned --json: %#v", args)
		}
	}
}

func calendarMCPRequest(arguments map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: arguments}}
}

func calendarCreateBaseArgs() map[string]any {
	return map[string]any{
		"calendar_id": "primary",
		"summary":     "Plan",
		"from":        "2026-08-04T09:00:00Z",
		"to":          "2026-08-04T10:00:00Z",
	}
}

func cloneCalendarArguments(arguments map[string]any) map[string]any {
	clone := make(map[string]any, len(arguments))
	for key, value := range arguments {
		clone[key] = value
	}
	return clone
}

func mergeCalendarArguments(base, extra map[string]any) map[string]any {
	merged := cloneCalendarArguments(base)
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

func calendarStringSlice(count int, prefix string) []string {
	values := make([]string, count)
	for i := range values {
		values[i] = prefix + "-" + string(rune('a'+i%26)) + "-" + strings.Repeat("x", i/26)
	}
	return values
}

func containsMCPArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func containsMCPName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func newCalendarMCPValidationClient(t *testing.T, toolNames []string, calls map[string]int) *mcpclient.Client {
	t.Helper()
	s := newMCPServer()
	for _, name := range toolNames {
		spec := findMCPTool(t, name)
		toolName := name
		s.AddTool(newMCPTool(spec), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			calls[toolName]++
			return mcp.NewToolResultText("reached"), nil
		})
	}
	client, err := mcpclient.NewInProcessClient(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	})
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "gog-calendar-test", Version: "1"}
	if _, err := client.Initialize(t.Context(), initRequest); err != nil {
		t.Fatal(err)
	}
	return client
}

func TestMCPWaveACalendarStructuredResultsThroughRunner(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		tool    string
		risk    string
		check   func(*testing.T, map[string]any)
	}{
		{
			name:    "C01 list calendars preserves page envelope",
			fixture: "list_calendars",
			tool:    "calendar_list_calendars",
			risk:    string(mcpRiskRead),
			check: func(t *testing.T, stdout map[string]any) {
				t.Helper()
				calendars := mcpCalendarResultArray(t, stdout["calendars"], "calendars")
				if len(calendars) != 1 {
					t.Fatalf("calendars = %#v, want one calendar", stdout["calendars"])
				}
				calendar := mcpCalendarResultObject(t, calendars[0], "calendars[0]")
				if calendar["id"] != "cal-1@example.com" || calendar["summary"] != "Team" || calendar["timeZone"] != "UTC" {
					t.Fatalf("calendar envelope = %#v", calendar)
				}
				if stdout["nextPageToken"] != "next-page" {
					t.Fatalf("nextPageToken = %#v, want next-page", stdout["nextPageToken"])
				}
			},
		},
		{
			name:    "M08 events preserves derived event fields",
			fixture: "events",
			tool:    "calendar_events",
			risk:    string(mcpRiskRead),
			check: func(t *testing.T, stdout map[string]any) {
				t.Helper()
				events := mcpCalendarResultArray(t, stdout["events"], "events")
				if len(events) != 1 {
					t.Fatalf("events = %#v, want one event", stdout["events"])
				}
				event := mcpCalendarResultObject(t, events[0], "events[0]")
				for field, want := range map[string]string{
					"id":             "event-list",
					"summary":        "Planning",
					"startDayOfWeek": "Tuesday",
					"endDayOfWeek":   "Tuesday",
					"timezone":       "UTC",
					"startLocal":     "2026-08-04T10:00:00Z",
					"endLocal":       "2026-08-04T11:00:00Z",
				} {
					if event[field] != want {
						t.Fatalf("events[0][%q] = %#v, want %q", field, event[field], want)
					}
				}
				if stdout["nextPageToken"] != "events-next" {
					t.Fatalf("nextPageToken = %#v, want events-next", stdout["nextPageToken"])
				}
			},
		},
		{
			name:    "C03 search preserves derived fields and redacts Zoom secrets",
			fixture: "search",
			tool:    "calendar_search_events",
			risk:    string(mcpRiskRead),
			check: func(t *testing.T, stdout map[string]any) {
				t.Helper()
				if stdout["query"] != "--planning" {
					t.Fatalf("query = %#v, want --planning", stdout["query"])
				}
				events := mcpCalendarResultArray(t, stdout["events"], "events")
				if len(events) != 1 {
					t.Fatalf("events = %#v, want one event", stdout["events"])
				}
				event := mcpCalendarResultObject(t, events[0], "events[0]")
				for field, want := range map[string]string{
					"id":             "event-search",
					"startDayOfWeek": "Tuesday",
					"endDayOfWeek":   "Tuesday",
					"timezone":       "America/New_York",
					"startLocal":     "2026-08-04T10:00:00-04:00",
					"endLocal":       "2026-08-04T11:00:00-04:00",
				} {
					if event[field] != want {
						t.Fatalf("search event[%q] = %#v, want %q", field, event[field], want)
					}
				}
				description, ok := event["description"].(string)
				if !ok || !strings.Contains(description, "pwd=REDACTED") || strings.Contains(description, "secret") {
					t.Fatalf("search description = %#v, want redacted Zoom password", event["description"])
				}
				conference := mcpCalendarResultObject(t, event["conferenceData"], "conferenceData")
				entryPoints := mcpCalendarResultArray(t, conference["entryPoints"], "conferenceData.entryPoints")
				entry := mcpCalendarResultObject(t, entryPoints[0], "conferenceData.entryPoints[0]")
				uri, _ := entry["uri"].(string)
				if !strings.Contains(uri, "pwd=REDACTED") || strings.Contains(uri, "secret") {
					t.Fatalf("search conference URI = %q, want redacted Zoom password", uri)
				}
			},
		},
		{
			name:    "C03 get event preserves derived fields and redacts Zoom secrets",
			fixture: "get_event",
			tool:    "calendar_get_event",
			risk:    string(mcpRiskRead),
			check: func(t *testing.T, stdout map[string]any) {
				t.Helper()
				event := mcpCalendarResultObject(t, stdout["event"], "event")
				if event["id"] != "event-get" || event["summary"] != "Planning" {
					t.Fatalf("event = %#v", event)
				}
				for field, want := range map[string]string{
					"startDayOfWeek": "Tuesday",
					"endDayOfWeek":   "Tuesday",
					"timezone":       "UTC",
					"eventTimezone":  "America/New_York",
					"startLocal":     "2026-08-04T14:00:00Z",
					"endLocal":       "2026-08-04T15:00:00Z",
				} {
					if event[field] != want {
						t.Fatalf("event[%q] = %#v, want %q", field, event[field], want)
					}
				}
				description, ok := event["description"].(string)
				if !ok || !strings.Contains(description, "pwd=REDACTED") || strings.Contains(description, "secret") {
					t.Fatalf("event description = %#v, want redacted Zoom password", event["description"])
				}
			},
		},
		{
			name:    "C04 freebusy preserves calendar blocks",
			fixture: "freebusy",
			tool:    "calendar_freebusy",
			risk:    string(mcpRiskRead),
			check: func(t *testing.T, stdout map[string]any) {
				t.Helper()
				calendars := mcpCalendarResultObject(t, stdout["calendars"], "calendars")
				team := mcpCalendarResultObject(t, calendars["team@example.com"], "calendars.team@example.com")
				busy := mcpCalendarResultArray(t, team["busy"], "calendars.team@example.com.busy")
				if len(busy) != 1 {
					t.Fatalf("team busy = %#v, want one block", team["busy"])
				}
				block := mcpCalendarResultObject(t, busy[0], "busy[0]")
				if block["start"] != "2026-08-04T09:00:00Z" || block["end"] != "2026-08-04T10:00:00Z" {
					t.Fatalf("freebusy block = %#v", block)
				}
			},
		},
		{
			name:    "C05 conflicts deduplicate overlaps in detection order",
			fixture: "conflicts",
			tool:    "calendar_find_conflicts",
			risk:    string(mcpRiskRead),
			check: func(t *testing.T, stdout map[string]any) {
				t.Helper()
				if stdout["count"] != json.Number("2") {
					t.Fatalf("conflict count = %#v, want 2", stdout["count"])
				}
				conflicts := mcpCalendarResultArray(t, stdout["conflicts"], "conflicts")
				if len(conflicts) != 2 {
					t.Fatalf("conflicts = %#v, want two deduplicated overlaps", stdout["conflicts"])
				}
				want := []struct {
					start, end string
				}{
					{start: "2026-08-04T09:45:00Z", end: "2026-08-04T10:00:00Z"},
					{start: "2026-08-04T09:45:00Z", end: "2026-08-04T10:15:00Z"},
				}
				for i, expected := range want {
					conflict := mcpCalendarResultObject(t, conflicts[i], "conflicts["+string(rune('0'+i))+"]")
					if conflict["start"] != expected.start || conflict["end"] != expected.end {
						t.Fatalf("conflicts[%d] = %#v, want %s-%s in detection order", i, conflict, expected.start, expected.end)
					}
					calendars := mcpCalendarResultArray(t, conflict["calendars"], "conflict calendars")
					if len(calendars) != 2 || calendars[0] != "a@example.com" || calendars[1] != "b@example.com" {
						t.Fatalf("conflicts[%d].calendars = %#v, want canonical pair", i, calendars)
					}
				}
			},
		},
		{
			name:    "C06 create event returns created event envelope",
			fixture: "create_event",
			tool:    "calendar_create_event",
			risk:    string(mcpRiskWrite),
			check: func(t *testing.T, stdout map[string]any) {
				t.Helper()
				event := mcpCalendarResultObject(t, stdout["event"], "event")
				if event["id"] != "created-event" || event["summary"] != "Plan" {
					t.Fatalf("created event = %#v", event)
				}
				start := mcpCalendarResultObject(t, event["start"], "event.start")
				if start["dateTime"] != "2026-08-04T09:00:00Z" {
					t.Fatalf("created event start = %#v", start)
				}
			},
		},
		{
			name:    "C08 respond returns attendee-only response envelope",
			fixture: "respond",
			tool:    "calendar_respond_to_event",
			risk:    string(mcpRiskWrite),
			check: func(t *testing.T, stdout map[string]any) {
				t.Helper()
				event := mcpCalendarResultObject(t, stdout["event"], "event")
				attendees := mcpCalendarResultArray(t, event["attendees"], "event.attendees")
				if len(attendees) != 1 {
					t.Fatalf("response attendees = %#v, want one attendee", event["attendees"])
				}
				attendee := mcpCalendarResultObject(t, attendees[0], "event.attendees[0]")
				if attendee["self"] != true || attendee["responseStatus"] != "accepted" || attendee["comment"] != "Thanks" {
					t.Fatalf("response attendee = %#v", attendee)
				}
			},
		},
		{
			name:    "C09 move returns moved event envelope",
			fixture: "move",
			tool:    "calendar_move_event",
			risk:    string(mcpRiskWrite),
			check: func(t *testing.T, stdout map[string]any) {
				t.Helper()
				event := mcpCalendarResultObject(t, stdout["event"], "event")
				if event["id"] != "moved-event" || event["summary"] != "Moved" {
					t.Fatalf("moved event = %#v", event)
				}
			},
		},
		{
			name:    "C10 create calendar returns calendar envelope",
			fixture: "create_calendar",
			tool:    "calendar_create_calendar",
			risk:    string(mcpRiskWrite),
			check: func(t *testing.T, stdout map[string]any) {
				t.Helper()
				calendar := mcpCalendarResultObject(t, stdout["calendar"], "calendar")
				if calendar["id"] != "created-calendar@example.com" || calendar["summary"] != "Team" || calendar["timeZone"] != "Europe/London" {
					t.Fatalf("created calendar = %#v", calendar)
				}
			},
		},
		{
			name:    "C11 subscribe returns raw-ID calendar envelope",
			fixture: "subscribe",
			tool:    "calendar_subscribe",
			risk:    string(mcpRiskWrite),
			check: func(t *testing.T, stdout map[string]any) {
				t.Helper()
				calendar := mcpCalendarResultObject(t, stdout["calendar"], "calendar")
				if calendar["id"] != "raw@example.com" || calendar["colorId"] != "24" || calendar["hidden"] != true {
					t.Fatalf("subscribed calendar = %#v", calendar)
				}
				if selected, present := calendar["selected"]; present && selected != false {
					t.Fatalf("subscribed calendar selected = %#v, want false or omitted", selected)
				}
			},
		},
		{
			name:    "C12 unsubscribe returns explicit result envelope",
			fixture: "unsubscribe",
			tool:    "calendar_unsubscribe",
			risk:    string(mcpRiskWrite),
			check: func(t *testing.T, stdout map[string]any) {
				t.Helper()
				if stdout["unsubscribed"] != true || stdout["calendarId"] != "raw@example.com" {
					t.Fatalf("unsubscribe result = %#v", stdout)
				}
			},
		},
		{
			name:    "C13 Focus Time returns specialized event envelope",
			fixture: "focus_time",
			tool:    "calendar_focus_time",
			risk:    string(mcpRiskWrite),
			check: func(t *testing.T, stdout map[string]any) {
				t.Helper()
				event := mcpCalendarResultObject(t, stdout["event"], "event")
				if event["id"] != "focus-event" || event["eventType"] != "focusTime" {
					t.Fatalf("focus event = %#v", event)
				}
				if _, ok := event["focusTimeProperties"].(map[string]any); !ok {
					t.Fatalf("focus event missing focusTimeProperties: %#v", event)
				}
			},
		},
		{
			name:    "C14 Out of Office returns specialized event envelope",
			fixture: "out_of_office",
			tool:    "calendar_out_of_office",
			risk:    string(mcpRiskWrite),
			check: func(t *testing.T, stdout map[string]any) {
				t.Helper()
				event := mcpCalendarResultObject(t, stdout["event"], "event")
				if event["id"] != "ooo-event" || event["eventType"] != "outOfOffice" {
					t.Fatalf("out-of-office event = %#v", event)
				}
				if _, ok := event["outOfOfficeProperties"].(map[string]any); !ok {
					t.Fatalf("out-of-office event missing outOfOfficeProperties: %#v", event)
				}
			},
		},
		{
			name:    "C15 Working Location returns specialized event envelope",
			fixture: "working_location",
			tool:    "calendar_working_location",
			risk:    string(mcpRiskWrite),
			check: func(t *testing.T, stdout map[string]any) {
				t.Helper()
				event := mcpCalendarResultObject(t, stdout["event"], "event")
				if event["id"] != "working-event" || event["eventType"] != "workingLocation" {
					t.Fatalf("working-location event = %#v", event)
				}
				if _, ok := event["workingLocationProperties"].(map[string]any); !ok {
					t.Fatalf("working-location event missing workingLocationProperties: %#v", event)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GOG_MCP_WAVE_A_CALENDAR_RESULT_HELPER", tt.fixture)
			result := mcpRunGogTool(t.Context(), mcpRunOptions{
				self:           os.Args[0],
				tool:           findMCPTool(t, tt.tool),
				commandArgs:    []string{"-test.run=TestMCPWaveACalendarResultRunnerHelper$"},
				timeout:        10 * time.Second,
				maxOutputBytes: 64 * 1024,
			})
			got := requireMCPWaveACalendarCommandResult(t, result)
			if result.IsError || got.ExitCode != 0 {
				t.Fatalf("%s result = %#v", tt.tool, got)
			}
			if got.Tool != tt.tool || got.Service != "calendar" || got.Risk != tt.risk {
				t.Fatalf("result metadata = tool %q service %q risk %q, want %q/calendar/%q", got.Tool, got.Service, got.Risk, tt.tool, tt.risk)
			}
			if got.Stderr != "calendar fixture stderr\n" {
				t.Fatalf("stderr = %q, want fixture diagnostics separate from stdout", got.Stderr)
			}
			stdout := mcpCalendarResultObject(t, got.Stdout, "stdout")
			tt.check(t, stdout)
		})
	}
}

func TestMCPWaveACalendarRespondDeclinedEventRefusalThroughRunner(t *testing.T) {
	t.Setenv("GOG_MCP_WAVE_A_CALENDAR_RESULT_HELPER", "respond_refusal")
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self:           os.Args[0],
		tool:           findMCPTool(t, "calendar_respond_to_event"),
		commandArgs:    []string{"-test.run=TestMCPWaveACalendarResultRunnerHelper$"},
		timeout:        10 * time.Second,
		maxOutputBytes: 16 * 1024,
	})
	got := requireMCPWaveACalendarCommandResult(t, result)
	if !result.IsError || got.ExitCode != 2 {
		t.Fatalf("declined-event refusal result = %#v, want MCP error exit 2", got)
	}
	if got.Stdout != nil {
		t.Fatalf("declined-event refusal unexpectedly emitted stdout: %#v", got.Stdout)
	}
	if !strings.Contains(got.Stderr, "not an attendee") || !strings.Contains(got.Stderr, "calendar fixture stderr") {
		t.Fatalf("declined-event refusal stderr = %q, want refusal and fixture diagnostics", got.Stderr)
	}
}

func TestMCPWaveACalendarResultRunnerHelper(t *testing.T) {
	fixture := os.Getenv("GOG_MCP_WAVE_A_CALENDAR_RESULT_HELPER")
	if fixture == "" {
		return
	}

	patchCalls := 0
	svc, _ := newCalendarServiceForTest(t, mcpCalendarFixtureHandler(fixture, &patchCalls))
	var args []string
	switch fixture {
	case "list_calendars":
		args = []string{"--json", "--account", "mcp@example.com", "calendar", "calendars", "--max", "2", "--page", "cursor"}
	case "events":
		args = []string{"--json", "--account", "mcp@example.com", "calendar", "events", "--from", "2026-08-04T00:00:00Z", "--to", "2026-08-05T00:00:00Z", "--max", "2", "--", "primary"}
	case "search":
		args = []string{"--json", "--account", "mcp@example.com", "calendar", "search", "--from", "2026-08-04T00:00:00Z", "--to", "2026-08-05T00:00:00Z", "--calendar", "primary", "--max", "2", "--", "--planning"}
	case "get_event":
		args = []string{"--json", "--account", "mcp@example.com", "calendar", "event", "--timezone", "UTC", "--", "primary", "event-get"}
	case "freebusy":
		args = []string{"--json", "--account", "mcp@example.com", "calendar", "freebusy", "--cal", "team@example.com", "--from", "2026-08-04T00:00:00Z", "--to", "2026-08-05T00:00:00Z", "--", "primary"}
	case "conflicts":
		args = []string{"--json", "--account", "mcp@example.com", "calendar", "conflicts", "--from", "2026-08-04T00:00:00Z", "--to", "2026-08-05T00:00:00Z", "--cal", "a@example.com", "--cal", "b@example.com"}
	case "create_event":
		args = []string{"--json", "--account", "mcp@example.com", "calendar", "create", "--summary", "Plan", "--from", "2026-08-04T09:00:00Z", "--to", "2026-08-04T10:00:00Z", "--send-updates", "none", "--", "primary"}
	case "respond":
		args = []string{"--json", "--account", "mcp@example.com", "calendar", "respond", "--status", "accepted", "--comment", "Thanks", "--", "primary", "event-respond"}
	case "respond_refusal":
		args = []string{"--json", "--account", "mcp@example.com", "calendar", "respond", "--status", "declined", "--", "primary", "event-refusal"}
	case "move":
		args = []string{"--json", "--account", "mcp@example.com", "calendar", "move", "--send-updates", "none", "--", "source@example.com", "event-move", "destination@example.com"}
	case "create_calendar":
		args = []string{"--json", "--account", "mcp@example.com", "calendar", "create-calendar", "--description", "Team calendar", "--timezone", "Europe/London", "--location", "London", "--", "Team"}
	case "subscribe":
		args = []string{"--json", "--account", "mcp@example.com", "calendar", "subscribe", "--color-id", "24", "--hidden", "--no-selected", "--", "raw@example.com"}
	case "unsubscribe":
		args = []string{"--json", "--account", "mcp@example.com", "calendar", "unsubscribe", "--force", "--", "raw@example.com"}
	case "focus_time":
		args = []string{"--json", "--account", "mcp@example.com", "calendar", "focus-time", "--summary", "Deep work", "--from", "2026-08-04T09:00:00Z", "--to", "2026-08-04T10:00:00Z", "--auto-decline", "new", "--chat-status", "available", "--decline-message", "Please do not book over focus time", "--rrule", "RRULE:FREQ=DAILY", "--", "primary"}
	case "out_of_office":
		args = []string{"--json", "--account", "mcp@example.com", "calendar", "out-of-office", "--summary", "Vacation", "--from", "2026-08-04T09:00:00Z", "--to", "2026-08-05T09:00:00Z", "--auto-decline", "none", "--decline-message", "Away", "--", "primary"}
	case "working_location":
		args = []string{"--json", "--account", "mcp@example.com", "calendar", "working-location", "--type", "office", "--from", "2026-08-04", "--to", "2026-08-05", "--office-label", "HQ", "--building-id", "B1", "--floor-id", "4", "--desk-id", "D42", "--", "primary"}
	default:
		t.Fatalf("unknown calendar fixture %q", fixture)
	}

	result := executeWithCalendarTestService(t, args, svc)
	if fixture == "respond_refusal" && patchCalls != 0 {
		_, _ = io.WriteString(os.Stderr, "respond refusal unexpectedly reached PATCH\n")
	}
	if result.err != nil {
		_, _ = io.WriteString(os.Stderr, result.err.Error()+"\n")
	}
	_, _ = io.WriteString(os.Stderr, "calendar fixture stderr\n")
	mcpNativeEmitExecuteResult(result)
}

func mcpCalendarFixtureHandler(fixture string, patchCalls *int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/calendar/v3")
		switch {
		case fixture == "list_calendars" && r.Method == http.MethodGet && path == "/users/me/calendarList":
			mcpWriteCalendarFixtureJSON(w, map[string]any{
				"items": []map[string]any{{
					"id": "cal-1@example.com", "summary": "Team", "timeZone": "UTC", "accessRole": "owner",
				}},
				"nextPageToken": "next-page",
			})
		case (fixture == "events" || fixture == "search") && r.Method == http.MethodGet && path == "/calendars/primary/events":
			if fixture == "search" {
				mcpWriteCalendarFixtureJSON(w, map[string]any{"items": []map[string]any{mcpCalendarZoomFixtureEvent("event-search", "Planning")}})
				return
			}
			mcpWriteCalendarFixtureJSON(w, map[string]any{
				"items":         []map[string]any{{"id": "event-list", "summary": "Planning", "start": map[string]any{"dateTime": "2026-08-04T10:00:00Z", "timeZone": "UTC"}, "end": map[string]any{"dateTime": "2026-08-04T11:00:00Z", "timeZone": "UTC"}}},
				"nextPageToken": "events-next",
			})
		case fixture == "get_event" && r.Method == http.MethodGet && path == "/calendars/primary/events/event-get":
			mcpWriteCalendarFixtureJSON(w, mcpCalendarZoomFixtureEvent("event-get", "Planning"))
		case fixture == "freebusy" && r.Method == http.MethodPost && path == "/freeBusy":
			mcpWriteCalendarFixtureJSON(w, map[string]any{
				"calendars": map[string]any{
					"primary":          map[string]any{"busy": []map[string]any{}},
					"team@example.com": map[string]any{"busy": []map[string]any{{"start": "2026-08-04T09:00:00Z", "end": "2026-08-04T10:00:00Z"}}},
				},
			})
		case fixture == "conflicts" && r.Method == http.MethodPost && path == "/freeBusy":
			mcpWriteCalendarFixtureJSON(w, map[string]any{
				"calendars": map[string]any{
					"a@example.com": map[string]any{"busy": []map[string]any{
						{"start": "2026-08-04T09:00:00Z", "end": "2026-08-04T10:00:00Z"},
						{"start": "2026-08-04T09:00:00Z", "end": "2026-08-04T10:00:00Z"},
						{"start": "2026-08-04T09:30:00Z", "end": "2026-08-04T10:30:00Z"},
					}},
					"b@example.com": map[string]any{"busy": []map[string]any{{"start": "2026-08-04T09:45:00Z", "end": "2026-08-04T10:15:00Z"}}},
				},
			})
		case fixture == "respond" && r.Method == http.MethodGet && path == "/calendars/primary/events/event-respond":
			mcpWriteCalendarFixtureJSON(w, map[string]any{
				"id": "event-respond", "summary": "Invitation",
				"attendees": []map[string]any{{"email": "mcp@example.com", "self": true, "responseStatus": "needsAction"}},
				"start":     map[string]any{"dateTime": "2026-08-04T09:00:00Z", "timeZone": "UTC"},
				"end":       map[string]any{"dateTime": "2026-08-04T10:00:00Z", "timeZone": "UTC"},
			})
		case (fixture == "respond" || fixture == "respond_refusal") && r.Method == http.MethodPatch && strings.HasSuffix(path, "/events/event-respond"):
			if patchCalls != nil {
				*patchCalls++
			}
			body := mcpCalendarFixtureRequestBody(r)
			body["id"] = "event-respond"
			body["summary"] = "Invitation"
			mcpWriteCalendarFixtureJSON(w, body)
		case fixture == "respond_refusal" && r.Method == http.MethodGet && path == "/calendars/primary/events/event-refusal":
			mcpWriteCalendarFixtureJSON(w, map[string]any{
				"id": "event-refusal", "summary": "Declined invitation",
				"attendees": []map[string]any{{"email": "organizer@example.com", "organizer": true, "responseStatus": "accepted"}},
			})
		case fixture == "move" && r.Method == http.MethodPost && strings.HasSuffix(path, "/events/event-move/move"):
			mcpWriteCalendarFixtureJSON(w, map[string]any{
				"id": "moved-event", "summary": "Moved",
				"start": map[string]any{"dateTime": "2026-08-04T09:00:00Z", "timeZone": "UTC"},
				"end":   map[string]any{"dateTime": "2026-08-04T10:00:00Z", "timeZone": "UTC"},
			})
		case fixture == "create_calendar" && r.Method == http.MethodPost && path == "/calendars":
			mcpWriteCalendarFixtureJSON(w, map[string]any{
				"id": "created-calendar@example.com", "summary": "Team", "description": "Team calendar",
				"timeZone": "Europe/London", "location": "London",
			})
		case fixture == "subscribe" && r.Method == http.MethodPost && path == "/users/me/calendarList":
			mcpWriteCalendarFixtureJSON(w, map[string]any{
				"id": "raw@example.com", "summary": "Raw", "colorId": "24", "hidden": true, "selected": false,
			})
		case fixture == "unsubscribe" && r.Method == http.MethodDelete && strings.HasPrefix(path, "/users/me/calendarList/"):
			mcpWriteCalendarFixtureJSON(w, map[string]any{})
		case (fixture == "create_event" || fixture == "focus_time" || fixture == "out_of_office" || fixture == "working_location") &&
			r.Method == http.MethodPost && strings.HasSuffix(path, "/events"):
			body := mcpCalendarFixtureRequestBody(r)
			switch fixture {
			case "create_event":
				body["id"] = "created-event"
			case "focus_time":
				body["id"] = "focus-event"
			case "out_of_office":
				body["id"] = "ooo-event"
			case "working_location":
				body["id"] = "working-event"
			}
			mcpWriteCalendarFixtureJSON(w, body)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/calendars/") && !strings.Contains(path, "/events"):
			mcpWriteCalendarFixtureJSON(w, map[string]any{"id": strings.TrimPrefix(path, "/calendars/"), "timeZone": "UTC"})
		default:
			http.NotFound(w, r)
		}
	})
}

func mcpCalendarFixtureRequestBody(r *http.Request) map[string]any {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body == nil {
		body = make(map[string]any)
	}
	if _, ok := body["summary"]; !ok {
		body["summary"] = "Calendar event"
	}
	return body
}

func mcpCalendarZoomFixtureEvent(id, summary string) map[string]any {
	return map[string]any{
		"id": id, "summary": summary,
		"description": "<!-- gog-zoom-meeting:1001 -->\nJoin Zoom Meeting: https://us02web.zoom.us/j/1001?pwd=secret\nMeeting ID: 1001\nPasscode: secret\n<!-- /gog-zoom-meeting -->",
		"conferenceData": map[string]any{
			"entryPoints": []map[string]any{{"entryPointType": "video", "uri": "https://us02web.zoom.us/j/1001?pwd=secret"}},
		},
		"start": map[string]any{"dateTime": "2026-08-04T14:00:00Z", "timeZone": "America/New_York"},
		"end":   map[string]any{"dateTime": "2026-08-04T15:00:00Z", "timeZone": "America/New_York"},
	}
}

func mcpWriteCalendarFixtureJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func requireMCPWaveACalendarCommandResult(t *testing.T, result *mcp.CallToolResult) mcpCommandResult {
	t.Helper()
	if result == nil {
		t.Fatal("nil MCP result")
	}
	got, ok := result.StructuredContent.(mcpCommandResult)
	if !ok {
		t.Fatalf("structured result type = %T, value=%#v", result.StructuredContent, result.StructuredContent)
	}
	return got
}

func mcpCalendarResultObject(t *testing.T, value any, field string) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s = %T (%#v), want object", field, value, value)
	}
	return object
}

func mcpCalendarResultArray(t *testing.T, value any, field string) []any {
	t.Helper()
	array, ok := value.([]any)
	if !ok {
		t.Fatalf("%s = %T (%#v), want array", field, value, value)
	}
	return array
}

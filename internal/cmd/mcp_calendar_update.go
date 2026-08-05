package cmd

import (
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

func mcpCalendarUpdateTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "calendar_update_event",
		Service:     "calendar",
		Risk:        mcpRiskWrite,
		Description: "Partially update an ordinary Google Calendar event by ID. Omitted fields are unchanged; requires --allow-write.",
		Options: []mcp.ToolOption{
			mcp.WithString("calendar_id", mcp.Description("Calendar ID"), mcp.Required()),
			mcp.WithString("event_id", mcp.Description("Event ID"), mcp.Required()),
			mcp.WithString("summary", mcp.Description("New event title; empty clears it")),
			mcp.WithString("start", mcp.Description("New start time (RFC3339 or date-only for all-day events)")),
			mcp.WithString("end", mcp.Description("New end time (RFC3339 or date-only for all-day events)")),
			mcp.WithString("start_timezone", mcp.Description("IANA timezone metadata for start; requires start")),
			mcp.WithString("end_timezone", mcp.Description("IANA timezone metadata for end; requires end")),
			mcp.WithString("description", mcp.Description("New description; empty clears it")),
			mcp.WithString("location", mcp.Description("New location; empty clears it")),
			mcp.WithString("attendees", mcp.Description("Comma-separated attendee emails replacing all; empty clears them")),
			mcp.WithString("add_attendees", mcp.Description("Comma-separated attendee emails to add while preserving existing attendees")),
			mcp.WithBoolean("all_day", mcp.Description("Set whether the event is all-day; requires start and end")),
			mcp.WithArray("rrule", mcp.Description("Recurrence rules; an empty array clears recurrence"), mcp.WithStringItems(), mcp.MaxItems(100)),
			mcp.WithArray("reminders", mcp.Description("Custom method:duration reminders; an empty array clears reminders"), mcp.WithStringItems(), mcp.MaxItems(5)),
			mcp.WithString("event_color", mcp.Description("Event color ID; empty clears it")),
			mcp.WithString("visibility", mcp.Description("Event visibility"), mcp.Enum("default", "public", "private", "confidential")),
			mcp.WithString("transparency", mcp.Description("Whether the event shows as busy or free"), mcp.Enum("opaque", "busy", "transparent", "free")),
			mcp.WithBoolean("guests_can_invite", mcp.Description("Allow guests to invite others")),
			mcp.WithBoolean("guests_can_modify", mcp.Description("Allow guests to modify the event")),
			mcp.WithBoolean("guests_can_see_others", mcp.Description("Allow guests to see other guests")),
			mcp.WithString("scope", mcp.Description("Recurring-event scope"), mcp.Enum("single", "future", "all"), mcp.DefaultString("all")),
			mcp.WithString("original_start", mcp.Description("Original start time of a recurring instance; required for single or future scope")),
			mcp.WithString("send_updates", mcp.Description("Attendee notification mode; defaults to none"), mcp.Enum("all", "externalOnly", "none"), mcp.DefaultString("none")),
		},
		BuildArgs: buildMCPCalendarUpdateArgs,
	}
}

func buildMCPCalendarUpdateArgs(req mcp.CallToolRequest) ([]string, error) {
	calendarID, err := requireMCPString(req, "calendar_id")
	if err != nil {
		return nil, err
	}
	eventID, err := requireMCPString(req, "event_id")
	if err != nil {
		return nil, err
	}

	args := []string{"calendar", "update"}

	optionalStrings := []struct {
		key  string
		flag string
	}{
		{key: "summary", flag: "--summary"},
		{key: "start", flag: "--from"},
		{key: "end", flag: "--to"},
		{key: "start_timezone", flag: "--start-timezone"},
		{key: "end_timezone", flag: "--end-timezone"},
		{key: "description", flag: "--description"},
		{key: "location", flag: "--location"},
	}
	stringPresent := make(map[string]bool, len(optionalStrings)+4)
	for _, field := range optionalStrings {
		value, present, valueErr := mcpCalendarUpdateOptionalString(req, field.key)
		if valueErr != nil {
			return nil, valueErr
		}
		if present {
			stringPresent[field.key] = true
			args = append(args, field.flag, value)
		}
	}
	if stringPresent["start_timezone"] && !stringPresent["start"] {
		return nil, fmt.Errorf("start_timezone requires start")
	}
	if stringPresent["end_timezone"] && !stringPresent["end"] {
		return nil, fmt.Errorf("end_timezone requires end")
	}

	attendees, attendeesPresent, err := mcpCalendarUpdateOptionalString(req, "attendees")
	if err != nil {
		return nil, err
	}
	addAttendees, addAttendeesPresent, err := mcpCalendarUpdateOptionalString(req, "add_attendees")
	if err != nil {
		return nil, err
	}
	if attendeesPresent && addAttendeesPresent {
		return nil, fmt.Errorf("attendees and add_attendees are mutually exclusive")
	}
	if attendeesPresent {
		args = append(args, "--attendees", attendees)
	}
	if addAttendeesPresent {
		if strings.TrimSpace(addAttendees) == "" {
			return nil, fmt.Errorf("add_attendees must not be empty")
		}
		args = append(args, "--add-attendee", addAttendees)
	}

	allDay, allDayPresent, err := mcpCalendarUpdateOptionalBool(req, "all_day")
	if err != nil {
		return nil, err
	}
	if allDayPresent {
		if !stringPresent["start"] || !stringPresent["end"] {
			return nil, fmt.Errorf("all_day requires start and end")
		}
		if allDay {
			args = append(args, "--all-day")
		} else {
			args = append(args, "--all-day=false")
		}
	}

	for _, field := range []struct {
		key   string
		flag  string
		limit int
	}{
		{key: "rrule", flag: "--rrule", limit: 100},
		{key: "reminders", flag: "--reminder", limit: 5},
	} {
		values, present, valuesErr := mcpCalendarUpdateOptionalStringArray(req, field.key, field.limit)
		if valuesErr != nil {
			return nil, valuesErr
		}
		if !present {
			continue
		}
		if len(values) == 0 {
			args = append(args, field.flag+"=")
			continue
		}
		for _, value := range values {
			args = append(args, field.flag, value)
		}
	}

	for _, field := range []struct {
		key  string
		flag string
	}{
		{key: "event_color", flag: "--event-color"},
		{key: "visibility", flag: "--visibility"},
		{key: "transparency", flag: "--transparency"},
	} {
		value, present, valueErr := mcpCalendarUpdateOptionalString(req, field.key)
		if valueErr != nil {
			return nil, valueErr
		}
		if present {
			args = append(args, field.flag, value)
		}
	}

	for _, field := range []struct {
		key  string
		flag string
	}{
		{key: "guests_can_invite", flag: "--guests-can-invite"},
		{key: "guests_can_modify", flag: "--guests-can-modify"},
		{key: "guests_can_see_others", flag: "--guests-can-see-others"},
	} {
		value, present, boolErr := mcpCalendarUpdateOptionalBool(req, field.key)
		if boolErr != nil {
			return nil, boolErr
		}
		if !present {
			continue
		}
		if value {
			args = append(args, field.flag)
		} else {
			args = append(args, "--no-"+strings.TrimPrefix(field.flag, "--"))
		}
	}

	scope, scopePresent, err := mcpCalendarUpdateOptionalString(req, "scope")
	if err != nil {
		return nil, err
	}
	if !scopePresent {
		scope = "all"
	}
	scope = strings.TrimSpace(scope)
	if validationErr := validateMCPEnum("scope", scope, "single", "future", "all"); validationErr != nil {
		return nil, validationErr
	}
	originalStart, originalStartPresent, err := mcpCalendarUpdateOptionalString(req, "original_start")
	if err != nil {
		return nil, err
	}
	if (scope == "single" || scope == "future") && (!originalStartPresent || strings.TrimSpace(originalStart) == "") {
		return nil, fmt.Errorf("original_start is required when scope=%s", scope)
	}
	if scopePresent {
		args = append(args, "--scope", scope)
	}
	if originalStartPresent {
		args = append(args, "--original-start", originalStart)
	}

	sendUpdates, sendUpdatesPresent, err := mcpCalendarUpdateOptionalString(req, "send_updates")
	if err != nil {
		return nil, err
	}
	if !sendUpdatesPresent {
		sendUpdates = "none"
	} else {
		sendUpdates = strings.TrimSpace(sendUpdates)
		if sendUpdates == "" {
			return nil, fmt.Errorf("send_updates must not be empty")
		}
	}
	if err := validateMCPEnum("send_updates", sendUpdates, "all", "externalOnly", "none"); err != nil {
		return nil, err
	}
	args = append(args, "--send-updates", sendUpdates)

	return append(args, "--", calendarID, eventID), nil
}

func mcpCalendarUpdateOptionalString(req mcp.CallToolRequest, key string) (string, bool, error) {
	raw, present := req.GetArguments()[key]
	if !present {
		return "", false, nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", true, fmt.Errorf("argument %q is not a string", key)
	}
	return value, true, nil
}

func mcpCalendarUpdateOptionalBool(req mcp.CallToolRequest, key string) (bool, bool, error) {
	raw, present := req.GetArguments()[key]
	if !present {
		return false, false, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return false, true, fmt.Errorf("argument %q is not a boolean", key)
	}
	return value, true, nil
}

func mcpCalendarUpdateOptionalStringArray(req mcp.CallToolRequest, key string, limit int) ([]string, bool, error) {
	if _, present := req.GetArguments()[key]; !present {
		return nil, false, nil
	}
	values, err := requireMCPOptionalStringArray(req, key)
	if err != nil {
		return nil, true, err
	}
	if limit > 0 && len(values) > limit {
		return nil, true, fmt.Errorf("%s must contain at most %d values", key, limit)
	}
	return values, true, nil
}

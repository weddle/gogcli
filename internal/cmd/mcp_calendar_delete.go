package cmd

import (
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

func mcpCalendarDeleteEventTool() mcpToolSpec {
	return mcpToolSpec{
		Name:        "calendar_delete_event",
		Service:     "calendar",
		Risk:        mcpRiskDestructive,
		Description: "Delete one Google Calendar event or recurring-event range by explicit IDs and scope. Requires ordinary write authorization plus an explicit destructive selector; the server supplies --force and recurrence resolution remains CLI-owned.",
		Options: []mcp.ToolOption{
			mcp.WithString("calendar_id", mcp.Description("Calendar ID or alias"), mcp.Required()),
			mcp.WithString("event_id", mcp.Description("Calendar event ID"), mcp.Required()),
			mcp.WithString("scope", mcp.Description("Recurring-event scope: single, future, or all"), mcp.Required(), mcp.Enum("single", "future", "all")),
			mcp.WithString("original_start", mcp.Description("Original start time of the recurring instance; required for single or future scope")),
			mcp.WithString("send_updates", mcp.Description("Attendee notification mode; defaults to none"), mcp.Enum("all", "externalOnly", "none"), mcp.DefaultString("none")),
		},
		BuildArgs: buildMCPCalendarDeleteEventArgs,
	}
}

func buildMCPCalendarDeleteEventArgs(req mcp.CallToolRequest) ([]string, error) {
	calendarID, err := requireMCPString(req, "calendar_id")
	if err != nil {
		return nil, err
	}
	eventID, err := requireMCPString(req, "event_id")
	if err != nil {
		return nil, err
	}
	scope, err := requireMCPString(req, "scope")
	if err != nil {
		return nil, err
	}
	scope = strings.TrimSpace(scope)
	if validationErr := validateMCPEnum("scope", scope, "single", "future", "all"); validationErr != nil {
		return nil, validationErr
	}

	originalStart, originalStartPresent, err := mcpCalendarUpdateOptionalString(req, "original_start")
	if err != nil {
		return nil, err
	}
	if originalStartPresent {
		originalStart = strings.TrimSpace(originalStart)
		if originalStart == "" {
			return nil, fmt.Errorf("original_start must not be empty when supplied")
		}
	}
	if scope == "all" {
		if originalStartPresent {
			return nil, fmt.Errorf("original_start is not allowed when scope=all")
		}
	} else if !originalStartPresent {
		return nil, fmt.Errorf("original_start is required when scope=%s", scope)
	}

	sendUpdates := "none"
	if raw, supplied := req.GetArguments()["send_updates"]; supplied {
		value, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("argument %q is not a string", "send_updates")
		}
		sendUpdates = strings.TrimSpace(value)
		if sendUpdates == "" {
			return nil, fmt.Errorf("send_updates must not be empty when supplied")
		}
	}
	if err := validateMCPEnum("send_updates", sendUpdates, "all", "externalOnly", "none"); err != nil {
		return nil, err
	}

	args := []string{"calendar", "delete", "--force", "--scope", scope, "--send-updates", sendUpdates}
	if originalStartPresent {
		args = append(args, "--original-start", originalStart)
	}
	return append(args, "--", calendarID, eventID), nil
}

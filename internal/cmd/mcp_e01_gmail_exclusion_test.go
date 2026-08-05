package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

var mcpE01ForbiddenGmailTools = []string{
	"gmail_send",
	"gmail_send_draft",
	"gmail_drafts_send",
	"gmail_post",
	"gmail_reply",
	"gmail_reply_all",
	"gmail_replyall",
	"gmail_forward",
	"gmail_fwd",
	"gmail_autoreply",
	"gmail_batch_delete",
	"gmail_delete_messages",
	"gmail_messages_delete",
	"gmail_permanent_delete",
	"gmail_permanent_delete_messages",
	"gmail_permanently_delete_messages",
	"gmail_message_permanent_delete",
	"send",
	"post",
	"reply",
	"reply-all",
	"replyall",
	"forward",
	"fwd",
	"autoreply",
	"permanent-delete",
}

func TestMCPE01GmailSendAndPermanentDeleteExcludedAcrossSelectors(t *testing.T) {
	selectors := []struct {
		name       string
		allowWrite bool
		selector   string
	}{
		{name: "read", selector: "read"},
		{name: "write", allowWrite: true, selector: "write"},
		{name: "gmail", allowWrite: true, selector: "gmail"},
		{name: "gmail wildcard", allowWrite: true, selector: "gmail.*"},
		{name: "destructive", allowWrite: true, selector: "destructive"},
		{name: "all", allowWrite: true, selector: "all"},
		{name: "star", allowWrite: true, selector: "*"},
		{name: "exact unknown", allowWrite: true, selector: "gmail_future_send_tool"},
	}

	assertNoForbidden := func(t *testing.T, source string, tools []mcpToolSpec) {
		t.Helper()
		for _, tool := range tools {
			if tool.Service == "gmail" && mcpE01ForbiddenGmailToolName(tool.Name) {
				t.Fatalf("%s registered forbidden Gmail tool %q", source, tool.Name)
			}
		}
		for _, name := range mcpE01ForbiddenGmailTools {
			if hasMCPTool(tools, name) {
				t.Fatalf("%s exposed forbidden Gmail tool %q in %#v", source, name, toolNames(tools))
			}
		}
	}

	assertToolsList := func(t *testing.T, source string, tools []mcpToolSpec) {
		t.Helper()
		var output bytes.Buffer
		if err := mcpPrintTools(&output, tools); err != nil {
			t.Fatalf("%s tools/list: %v", source, err)
		}
		var payload struct {
			Tools []struct {
				Name    string `json:"name"`
				Service string `json:"service"`
			} `json:"tools"`
		}
		if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
			t.Fatalf("%s tools/list JSON: %v", source, err)
		}
		if len(payload.Tools) != len(tools) {
			t.Fatalf("%s tools/list count = %d, want %d", source, len(payload.Tools), len(tools))
		}
		listed := make([]mcpToolSpec, 0, len(payload.Tools))
		for _, item := range payload.Tools {
			listed = append(listed, mcpToolSpec{Name: item.Name, Service: item.Service})
		}
		assertNoForbidden(t, source+" tools/list", listed)
	}

	assertNoForbidden(t, "registry", mcpAllTools())
	for _, tt := range selectors {
		t.Run(tt.name, func(t *testing.T) {
			tools := mcpEnabledTools(McpCmd{AllowWrite: tt.allowWrite, AllowTool: []string{tt.selector}})
			assertNoForbidden(t, "selector "+tt.selector, tools)
			assertToolsList(t, "selector "+tt.selector, tools)
		})
	}
	for _, name := range mcpE01ForbiddenGmailTools {
		t.Run("exact "+name, func(t *testing.T) {
			tools := mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{name}})
			assertNoForbidden(t, "exact selector "+name, tools)
			assertToolsList(t, "exact selector "+name, tools)
		})
	}
}

func mcpE01ForbiddenGmailToolName(name string) bool {
	name = strings.ToLower(strings.ReplaceAll(name, "_", "-"))
	for _, token := range []string{
		"send",
		"post",
		"reply",
		"permanent",
		"batch-delete",
		"delete-messages",
		"messages-delete",
		"message-delete",
	} {
		if strings.Contains(name, token) {
			return true
		}
	}
	return false
}

package cmd

import (
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

type mcpE02GmailMutationFixture struct {
	arguments map[string]any
	wantArgv  []string
}

func mcpE02GmailMutationFixtures() map[string]mcpE02GmailMutationFixture {
	return map[string]mcpE02GmailMutationFixture{
		"gmail_create_draft": {
			arguments: map[string]any{"subject": "Subject", "body": "Body"},
			wantArgv:  []string{"gmail", "drafts", "create", "--subject", "Subject", "--body", "Body"},
		},
		"gmail_update_draft": {
			arguments: map[string]any{"draft_id": "d1", "subject": "Subject", "body": "Body"},
			wantArgv:  []string{"gmail", "drafts", "update", "--subject", "Subject", "--body", "Body", "--", "d1"},
		},
		"gmail_modify_message_labels": {
			arguments: map[string]any{"message_id": "m1", "add": "STARRED"},
			wantArgv:  []string{"gmail", "messages", "modify", "--add", "STARRED", "--", "m1"},
		},
		"gmail_modify_thread_labels": {
			arguments: map[string]any{"thread_id": "t1", "remove": "INBOX"},
			wantArgv:  []string{"gmail", "thread", "modify", "--remove", "INBOX", "--", "t1"},
		},
		"gmail_archive_messages": {
			arguments: map[string]any{"message_ids": []string{"m1", "m2"}},
			wantArgv:  []string{"gmail", "archive", "m1", "m2"},
		},
		"gmail_archive_threads": {
			arguments: map[string]any{"thread_ids": []string{"t1", "t2"}},
			wantArgv:  []string{"gmail", "archive", "--thread", "t1", "t2"},
		},
		"gmail_mark_messages_read": {
			arguments: map[string]any{"message_ids": []string{"m1"}},
			wantArgv:  []string{"gmail", "mark-read", "--", "m1"},
		},
		"gmail_mark_messages_unread": {
			arguments: map[string]any{"message_ids": []string{"m1"}},
			wantArgv:  []string{"gmail", "unread", "--", "m1"},
		},
	}
}

func cloneMCPE02Arguments(arguments map[string]any) map[string]any {
	clone := make(map[string]any, len(arguments)+1)
	for key, value := range arguments {
		clone[key] = value
	}
	return clone
}

func TestMCPE02GmailMutationRegistryAndArgvRejectQueryExpansion(t *testing.T) {
	fixtures := mcpE02GmailMutationFixtures()
	seen := make(map[string]bool, len(fixtures))

	for _, spec := range mcpAllTools() {
		if spec.Service != "gmail" || spec.Risk == mcpRiskRead {
			continue
		}
		fixture, ok := fixtures[spec.Name]
		if !ok {
			t.Fatalf("Gmail mutation %q has no E02 explicit-ID fixture", spec.Name)
		}
		seen[spec.Name] = true

		schema := newMCPTool(spec).InputSchema
		if closed, ok := schema.AdditionalProperties.(bool); !ok || closed {
			t.Fatalf("%s schema AdditionalProperties = %#v, want false", spec.Name, schema.AdditionalProperties)
		}
		for _, forbidden := range []string{"query", "max"} {
			if _, exposed := schema.Properties[forbidden]; exposed {
				t.Fatalf("%s schema exposes query expansion field %q", spec.Name, forbidden)
			}
		}

		args, err := spec.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: fixture.arguments}})
		if err != nil {
			t.Fatalf("%s BuildArgs: %v", spec.Name, err)
		}
		if got, want := strings.Join(args, "\x00"), strings.Join(fixture.wantArgv, "\x00"); got != want {
			t.Fatalf("%s argv = %#v, want %#v", spec.Name, args, fixture.wantArgv)
		}
		for _, arg := range args {
			if arg == "--query" || arg == "--max" {
				t.Fatalf("%s argv exposes query expansion flag: %#v", spec.Name, args)
			}
		}
	}
	for name := range fixtures {
		if !seen[name] {
			t.Fatalf("expected Gmail mutation %q in MCP registry", name)
		}
	}

	for _, selector := range []string{"gmail", "gmail.*", "write", "all"} {
		tools := mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{selector}})
		for name := range fixtures {
			if !hasMCPTool(tools, name) {
				t.Errorf("authorized selector %q omitted registered Gmail mutation %q", selector, name)
			}
		}
	}
}

func TestMCPE02GmailMutationSchemasRejectQueryAndMaxBeforeChild(t *testing.T) {
	fixtures := mcpE02GmailMutationFixtures()
	for toolName, fixture := range fixtures {
		t.Run(toolName, func(t *testing.T) {
			for _, forbidden := range []struct {
				name  string
				value any
			}{
				{name: "query", value: "is:unread"},
				{name: "max", value: 100},
			} {
				t.Run(forbidden.name, func(t *testing.T) {
					arguments := cloneMCPE02Arguments(fixture.arguments)
					arguments[forbidden.name] = forbidden.value
					result, calls := callMCPGmailSchema(t, toolName, arguments)
					if !result.IsError || calls != 0 || !strings.Contains(mcpResultText(result), forbidden.name) {
						t.Fatalf("%s result = %#v, handler calls = %d, want schema rejection before child", forbidden.name, result.Content, calls)
					}
				})
			}
		})
	}
}

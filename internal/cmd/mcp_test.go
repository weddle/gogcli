package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/steipete/gogcli/internal/config"
	"github.com/steipete/gogcli/internal/googleapi"
)

func TestMCPToolRiskAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		risk        mcpToolRisk
		readOnly    bool
		destructive bool
		idempotent  bool
		openWorld   bool
	}{
		{name: "read", risk: mcpRiskRead, readOnly: true, destructive: false, idempotent: true, openWorld: true},
		{name: "write", risk: mcpRiskWrite, readOnly: false, destructive: false, idempotent: false, openWorld: true},
		{name: "destructive", risk: mcpRiskDestructive, readOnly: false, destructive: true, idempotent: false, openWorld: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := newMCPTool(mcpToolSpec{Name: "annotation_test", Risk: tt.risk})
			assertMCPHint := func(name string, got *bool, want bool) {
				t.Helper()
				if got == nil {
					t.Fatalf("%s annotation is nil", name)
				}
				if *got != want {
					t.Fatalf("%s annotation = %t, want %t", name, *got, want)
				}
			}
			assertMCPHint("readOnlyHint", tool.Annotations.ReadOnlyHint, tt.readOnly)
			assertMCPHint("destructiveHint", tool.Annotations.DestructiveHint, tt.destructive)
			assertMCPHint("idempotentHint", tool.Annotations.IdempotentHint, tt.idempotent)
			assertMCPHint("openWorldHint", tool.Annotations.OpenWorldHint, tt.openWorld)
		})
	}
}

func TestMCPRegistryDestructiveInventoryAndPolicy(t *testing.T) {
	want := []string{
		"gmail_delete_draft",
		"gmail_trash_messages",
		"calendar_delete_event",
		"drive_trash",
		"drive_share_user",
		"drive_unshare",
	}
	counts := map[mcpToolRisk]int{}
	var got []string
	for _, tool := range mcpAllTools() {
		counts[tool.Risk]++
		if tool.Risk == mcpRiskDestructive {
			got = append(got, tool.Name)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("destructive registry = %#v, want %#v", got, want)
	}
	if counts[mcpRiskRead] != 19 || counts[mcpRiskWrite] != 29 || counts[mcpRiskDestructive] != len(want) {
		t.Fatalf("registry risk counts = %#v, want read=19 write=29 destructive=%d", counts, len(want))
	}

	for _, selector := range []string{"write", "gmail", "gmail.*", "calendar", "drive", "drive.*", "all", "*"} {
		tools := mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{selector}})
		for _, name := range want {
			if hasMCPTool(tools, name) {
				t.Fatalf("broad selector %q exposed destructive tool %q", selector, name)
			}
		}
	}
	if tools := mcpEnabledTools(McpCmd{AllowTool: []string{"destructive"}}); len(tools) != 0 {
		t.Fatalf("destructive selector without write authorization exposed %#v", toolNames(tools))
	}
	if tools := mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{"destructive"}}); !slices.Equal(toolNames(tools), want) {
		t.Fatalf("destructive selector inventory = %#v, want %#v", toolNames(tools), want)
	}
	for _, name := range want {
		tools := mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{name}})
		if !hasMCPTool(tools, name) {
			t.Fatalf("exact destructive selector omitted %q", name)
		}
	}
	if tools := mcpEnabledToolsNoPolicy(McpCmd{AllowWrite: true, AllowTool: []string{"destructive"}}, &RootFlags{ReadOnly: true}); len(tools) != 0 {
		t.Fatalf("readonly runtime exposed destructive tools: %#v", toolNames(tools))
	}
}

func TestMCPDestructiveSelectionRequiresWriteAndExplicitSelector(t *testing.T) {
	tool := mcpToolSpec{
		Name:    "calendar_delete_event",
		Service: "calendar",
		Risk:    mcpRiskDestructive,
	}
	tests := []struct {
		name        string
		allowWrite  bool
		selectors   []string
		wantVisible bool
	}{
		{name: "default", selectors: nil},
		{name: "write authorization only", allowWrite: true},
		{name: "empty selector", allowWrite: true, selectors: []string{}},
		{name: "read selector", allowWrite: true, selectors: []string{"read"}},
		{name: "write selector", allowWrite: true, selectors: []string{"write"}},
		{name: "service selector", allowWrite: true, selectors: []string{"calendar"}},
		{name: "service wildcard", allowWrite: true, selectors: []string{"calendar.*"}},
		{name: "star selector", allowWrite: true, selectors: []string{"*"}},
		{name: "all selector", allowWrite: true, selectors: []string{"all"}},
		{name: "destructive wildcard", allowWrite: true, selectors: []string{"destructive.*"}},
		{name: "unknown selector", allowWrite: true, selectors: []string{"future_tool"}},
		{name: "destructive risk selector", allowWrite: true, selectors: []string{"destructive"}, wantVisible: true},
		{name: "exact tool selector", allowWrite: true, selectors: []string{"calendar_delete_event"}, wantVisible: true},
		{name: "exact without write authorization", selectors: []string{"calendar_delete_event"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mcpToolVisible(tool, tt.allowWrite, tt.selectors); got != tt.wantVisible {
				t.Fatalf("mcpToolVisible(%t, %#v) = %t, want %t", tt.allowWrite, tt.selectors, got, tt.wantVisible)
			}
		})
	}
}

func TestMCPPolicyAcceptsExplicitDestructiveSelector(t *testing.T) {
	policy, err := normalizeMCPPolicy(config.MCPPolicy{
		AllowTools: []string{"destructive"},
		AllowWrite: true,
	})
	if err != nil {
		t.Fatalf("normalizeMCPPolicy: %v", err)
	}
	if len(policy.AllowTools) != 1 || policy.AllowTools[0] != "destructive" {
		t.Fatalf("normalized destructive policy = %#v", policy)
	}
	if !mcpSelectorMatchesAnyTool("destructive") {
		t.Fatal("destructive selector should be recognized before a domain tool is registered")
	}

	accountPolicy, err := selectMCPPolicy(config.MCPConfig{
		MCPPolicy: config.MCPPolicy{AllowTools: []string{"read"}},
		Accounts: map[string]config.MCPPolicy{
			"destructive@example.com": policy,
		},
	}, "destructive@example.com")
	if err != nil {
		t.Fatalf("selectMCPPolicy: %v", err)
	}
	tool := mcpToolSpec{Name: "calendar_delete_event", Service: "calendar", Risk: mcpRiskDestructive}
	if !mcpToolVisible(tool, accountPolicy.AllowWrite, accountPolicy.AllowTools) {
		t.Fatal("explicit per-account destructive policy should select a destructive tool")
	}
	if mcpToolVisible(tool, false, accountPolicy.AllowTools) {
		t.Fatal("readonly policy must suppress destructive tools")
	}

	for _, selectors := range [][]string{{}, {"unknown_selector"}, {"destructive.*"}} {
		if _, err := normalizeMCPPolicy(config.MCPPolicy{AllowTools: selectors, AllowWrite: true}); err == nil {
			t.Fatalf("expected fail-closed policy for selectors %#v", selectors)
		}
	}
}

func TestMCPEnabledToolsDefaultReadOnly(t *testing.T) {
	tools := mcpEnabledTools(McpCmd{})
	if len(tools) == 0 {
		t.Fatal("expected default tools")
	}
	for _, tool := range tools {
		if tool.Risk != mcpRiskRead {
			t.Fatalf("default enabled write tool %s", tool.Name)
		}
	}
	if hasMCPTool(tools, "docs_write") {
		t.Fatal("docs_write should require --allow-write")
	}
	if !hasMCPTool(tools, "gmail_search") {
		t.Fatal("gmail_search should be enabled by default")
	}
}

func TestMCPRuntimeReadonlyWithoutPolicySuppressesWrites(t *testing.T) {
	tools := mcpEnabledToolsNoPolicy(McpCmd{
		AllowWrite: true,
		AllowTool:  []string{"all"},
	}, &RootFlags{ReadOnly: true})
	if len(tools) == 0 {
		t.Fatal("expected read tools")
	}
	for _, tool := range tools {
		if tool.Risk != mcpRiskRead {
			t.Fatalf("readonly runtime exposed %s tool %q", tool.Risk, tool.Name)
		}
	}
	if hasMCPTool(tools, "docs_write") {
		t.Fatal("readonly runtime exposed docs_write without persistent policy")
	}
}

func TestMCPEnabledToolsAllowWriteAndFilter(t *testing.T) {
	tools := mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{"docs.*"}})
	if !hasMCPTool(tools, "docs_get") || !hasMCPTool(tools, "docs_write") {
		t.Fatalf("expected docs read and write tools, got %#v", toolNames(tools))
	}
	if hasMCPTool(tools, "gmail_search") {
		t.Fatalf("gmail tool leaked through docs filter: %#v", toolNames(tools))
	}
}

func TestMCPPolicyDefaultsToReadOnly(t *testing.T) {
	policy, err := selectMCPPolicy(config.MCPConfig{}, "")
	if err != nil {
		t.Fatalf("selectMCPPolicy: %v", err)
	}
	tools, err := mcpEnabledToolsWithPolicy(McpCmd{}, &RootFlags{}, policy)
	if err != nil {
		t.Fatalf("mcpEnabledToolsWithPolicy: %v", err)
	}
	if !hasMCPTool(tools, "gmail_search") || hasMCPTool(tools, "docs_write") {
		t.Fatalf("unexpected default policy tools: %#v", toolNames(tools))
	}
}

func TestMCPPolicyAccountReplacesGlobalAndEnablesNarrowWrites(t *testing.T) {
	cfg := config.MCPConfig{
		MCPPolicy: config.MCPPolicy{AllowTools: []string{"read"}},
		Accounts: map[string]config.MCPPolicy{
			" Personal@Example.com ": {AllowTools: []string{"docs.*"}, AllowWrite: true},
		},
	}
	policy, err := selectMCPPolicy(cfg, "personal@example.com")
	if err != nil {
		t.Fatalf("selectMCPPolicy: %v", err)
	}
	tools, err := mcpEnabledToolsWithPolicy(McpCmd{}, &RootFlags{}, policy)
	if err != nil {
		t.Fatalf("mcpEnabledToolsWithPolicy: %v", err)
	}
	if !hasMCPTool(tools, "docs_get") || !hasMCPTool(tools, "docs_write") {
		t.Fatalf("expected configured Docs tools: %#v", toolNames(tools))
	}
	if hasMCPTool(tools, "gmail_search") {
		t.Fatalf("global policy leaked into account replacement: %#v", toolNames(tools))
	}
}

func TestMCPPolicyRuntimeCanOnlyNarrow(t *testing.T) {
	policy, err := normalizeMCPPolicy(config.MCPPolicy{AllowTools: []string{"docs.*"}, AllowWrite: true})
	if err != nil {
		t.Fatalf("normalizeMCPPolicy: %v", err)
	}
	tools, err := mcpEnabledToolsWithPolicy(McpCmd{AllowTool: []string{"docs_get"}}, &RootFlags{}, policy)
	if err != nil {
		t.Fatalf("mcpEnabledToolsWithPolicy: %v", err)
	}
	if got := toolNames(tools); len(got) != 1 || got[0] != "docs_get" {
		t.Fatalf("runtime narrowed tools = %#v", got)
	}

	_, err = mcpEnabledToolsWithPolicy(McpCmd{AllowWrite: true}, &RootFlags{}, config.MCPPolicy{AllowTools: []string{"read"}})
	if err == nil || !strings.Contains(err.Error(), "cannot widen") {
		t.Fatalf("allow-write widening error = %v", err)
	}
}

func TestMCPPolicyReadOnlyRootHidesConfiguredWrites(t *testing.T) {
	policy, err := normalizeMCPPolicy(config.MCPPolicy{AllowTools: []string{"docs.*"}, AllowWrite: true})
	if err != nil {
		t.Fatalf("normalizeMCPPolicy: %v", err)
	}
	tools, err := mcpEnabledToolsWithPolicy(McpCmd{}, &RootFlags{ReadOnly: true}, policy)
	if err != nil {
		t.Fatalf("mcpEnabledToolsWithPolicy: %v", err)
	}
	if !hasMCPTool(tools, "docs_get") || hasMCPTool(tools, "docs_write") {
		t.Fatalf("readonly tools = %#v", toolNames(tools))
	}
}

func TestMCPPolicyRejectsUnsafeOrUnknownConfig(t *testing.T) {
	for _, policy := range []config.MCPPolicy{
		{AllowWrite: true},
		{AllowTools: []string{}},
		{AllowTools: []string{"not_a_tool"}},
	} {
		if _, err := normalizeMCPPolicy(policy); err == nil {
			t.Fatalf("expected policy error for %#v", policy)
		}
	}

	duplicateAccount := " user@example.com "
	_, err := selectMCPPolicy(config.MCPConfig{Accounts: map[string]config.MCPPolicy{
		"User@example.com": {},
		duplicateAccount:   {},
	}}, "user@example.com")
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate account error = %v", err)
	}

	_, err = selectMCPPolicy(config.MCPConfig{
		Accounts: map[string]config.MCPPolicy{
			"selected@example.com": {AllowTools: []string{"read"}},
			"other@example.com":    {AllowTools: []string{"not_a_tool"}},
		},
	}, "selected@example.com")
	if err == nil || !strings.Contains(err.Error(), "other@example.com") || !strings.Contains(err.Error(), "matches no tool") {
		t.Fatalf("unselected account validation error = %v", err)
	}
}

func TestMCPPolicyAccountResolutionPinsAliasAndRejectsUnverifiableIdentity(t *testing.T) {
	store := config.NewConfigStore(config.Layout{ConfigDir: t.TempDir()})
	if err := store.Write(config.File{AccountAliases: map[string]string{"personal": "Personal@Example.com"}}); err != nil {
		t.Fatalf("write config: %v", err)
	}
	flags := &RootFlags{
		Account: "personal",
		configStoreResolver: func() (*config.ConfigStore, error) {
			return store, nil
		},
	}
	account, err := resolveMCPPolicyAccount(flags)
	if err != nil {
		t.Fatalf("resolveMCPPolicyAccount: %v", err)
	}
	if account != "Personal@Example.com" {
		t.Fatalf("resolved account = %q", account)
	}

	for _, unverifiable := range []*RootFlags{
		{AccessToken: "token", Account: "label@example.com"},
		{authMode: googleapi.AuthModeADC, Account: "label@example.com"},
	} {
		account, err := resolveMCPPolicyAccount(unverifiable)
		if err != nil || account != "" {
			t.Fatalf("unverifiable identity resolution = %q, %v", account, err)
		}
	}
}

func TestMCPListToolsUsesRuntimeStdout(t *testing.T) {
	var output bytes.Buffer
	err := (&McpCmd{
		ListTools:      true,
		TimeoutSeconds: 60,
		MaxOutputBytes: 1024,
	}).Run(newCmdRuntimeOutputContext(t, &output, io.Discard), &RootFlags{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := output.String(); !strings.Contains(got, `"tools"`) || !strings.Contains(got, `"gmail_search"`) {
		t.Fatalf("unexpected tool list: %s", got)
	}
}

func TestMCPParentArgsPreserveContextAndSafety(t *testing.T) {
	flags := &RootFlags{
		Home:                "/tmp/gog-home",
		Account:             "bot@example.com",
		Client:              "test-client",
		ResultsOnly:         true,
		Select:              "messages",
		DryRun:              true,
		GmailNoSend:         true,
		ReadOnly:            true,
		EnableCommands:      "gmail.search,docs.cat",
		EnableCommandsExact: "mcp,gmail.messages.search",
		DisableCommands:     "drive.delete",
	}
	base := strings.Join(mcpParentRootArgs(flags), "\x00")
	for _, want := range []string{"--json", "--wrap-untrusted", "--no-input", "--color=never", "--home\x00/tmp/gog-home", "--account\x00bot@example.com", "--client\x00test-client", "--results-only", "--select\x00messages", "--dry-run"} {
		if !strings.Contains(base, want) {
			t.Fatalf("base args missing %q in %#v", want, mcpParentRootArgs(flags))
		}
	}
	safety := strings.Join(mcpParentSafetyArgs(flags), "\x00")
	for _, want := range []string{"--gmail-no-send", "--readonly", "--enable-commands=gmail.search,docs.cat", "--enable-commands-exact=mcp,gmail.messages.search", "--disable-commands=drive.delete"} {
		if !strings.Contains(safety, want) {
			t.Fatalf("safety args missing %q in %#v", want, mcpParentSafetyArgs(flags))
		}
	}
}

func TestMCPDriveSearchBuildArgsExact(t *testing.T) {
	tool := findMCPTool(t, "drive_search")
	tests := []struct {
		name      string
		arguments map[string]any
		want      []string
	}{
		{
			name: "bounded max",
			arguments: map[string]any{
				"query": "report",
				"max":   250,
			},
			want: []string{"drive", "search", "--max", "100", "--", "report"},
		},
		{
			name: "raw query",
			arguments: map[string]any{
				"query":     "mimeType = 'application/pdf'",
				"raw_query": true,
			},
			want: []string{"drive", "search", "--max", "20", "--raw-query", "--", "mimeType = 'application/pdf'"},
		},
		{
			name: "parent",
			arguments: map[string]any{
				"query":  "report",
				"parent": "folder-123",
			},
			want: []string{"drive", "search", "--max", "20", "--parent", "folder-123", "--", "report"},
		},
		{
			name: "query delimiter",
			arguments: map[string]any{
				"query": "--parent",
			},
			want: []string{"drive", "search", "--max", "20", "--", "--parent"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tt.arguments}})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(args, "\x00") != strings.Join(tt.want, "\x00") {
				t.Fatalf("args = %#v, want %#v", args, tt.want)
			}
		})
	}
}

func TestMCPDriveSearchRejectsRawQueryWithParent(t *testing.T) {
	tool := findMCPTool(t, "drive_search")
	args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"query":     "mimeType = 'application/pdf'",
			"raw_query": true,
			"parent":    "folder-123",
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "--parent") || !strings.Contains(err.Error(), "--raw-query") {
		t.Fatalf("expected raw query/parent conflict, got args=%#v err=%v", args, err)
	}
	if args != nil {
		t.Fatalf("conflicting arguments returned argv: %#v", args)
	}
}

func TestMCPDriveListFolderBuildArgsExact(t *testing.T) {
	tool := findMCPTool(t, "drive_list_folder")
	tests := []struct {
		name      string
		arguments map[string]any
		want      []string
	}{
		{
			name:      "default root",
			arguments: map[string]any{},
			want:      []string{"drive", "ls", "--max", "20"},
		},
		{
			name: "folder and bounded max",
			arguments: map[string]any{
				"folder_id": "folder-123",
				"max":       250,
				"args":      []any{"drive", "delete", "file"},
			},
			want: []string{"drive", "ls", "--max", "100", "--parent", "folder-123"},
		},
		{
			name: "lower-bounded max",
			arguments: map[string]any{
				"max": 0,
			},
			want: []string{"drive", "ls", "--max", "1"},
		},
		{
			name: "page token",
			arguments: map[string]any{
				"page_token": "next-page",
			},
			want: []string{"drive", "ls", "--max", "20", "--page", "next-page"},
		},
		{
			name: "exclude shared drives",
			arguments: map[string]any{
				"include_shared_drives": false,
				"max":                   3,
			},
			want: []string{"drive", "ls", "--max", "3", "--no-all-drives"},
		},
		{
			name: "all options",
			arguments: map[string]any{
				"folder_id":             " shared-folder ",
				"max":                   7,
				"page_token":            " page-2 ",
				"include_shared_drives": true,
			},
			want: []string{"drive", "ls", "--max", "7", "--page", "page-2", "--parent", "shared-folder"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tt.arguments}})
			if err != nil {
				t.Fatal(err)
			}
			if got, want := strings.Join(args, "\x00"), strings.Join(tt.want, "\x00"); got != want {
				t.Fatalf("argv = %#v, want %#v", args, tt.want)
			}
		})
	}
}

func TestMCPDriveListFolderIsDefaultReadOnly(t *testing.T) {
	tool := findMCPTool(t, "drive_list_folder")
	if tool.Risk != mcpRiskRead {
		t.Fatalf("risk = %q, want %q", tool.Risk, mcpRiskRead)
	}
	if !hasMCPTool(mcpEnabledTools(McpCmd{}), tool.Name) {
		t.Fatalf("%s missing from default tools", tool.Name)
	}
}

func TestMCPDriveListFolderInputSchemaIsClosed(t *testing.T) {
	s := newMCPServer()
	handlerCalls := 0
	s.AddTool(newMCPTool(findMCPTool(t, "drive_list_folder")), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handlerCalls++
		return mcp.NewToolResultText("ok"), nil
	})

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
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "gog-test", Version: "1"}
	if _, err := client.Initialize(t.Context(), initRequest); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		arguments map[string]any
		wantError bool
		wantText  string
	}{
		{
			name: "unknown args field",
			arguments: map[string]any{
				"args": []any{"drive", "delete", "file"},
			},
			wantError: true,
			wantText:  "args",
		},
		{
			name: "unknown query field",
			arguments: map[string]any{
				"query": "trashed = false",
			},
			wantError: true,
			wantText:  "query",
		},
		{
			name: "unknown fields field",
			arguments: map[string]any{
				"fields": "files(id,name)",
			},
			wantError: true,
			wantText:  "fields",
		},
		{
			name: "unknown all field",
			arguments: map[string]any{
				"all": true,
			},
			wantError: true,
			wantText:  "all",
		},
		{
			name: "wrong max type",
			arguments: map[string]any{
				"max": "10",
			},
			wantError: true,
			wantText:  "max",
		},
		{
			name:      "all optional fields omitted",
			arguments: map[string]any{},
			wantText:  "ok",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := handlerCalls
			result, err := client.CallTool(t.Context(), mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "drive_list_folder",
					Arguments: tt.arguments,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError != tt.wantError {
				t.Fatalf("IsError = %v, want %v: %#v", result.IsError, tt.wantError, result.Content)
			}
			if tt.wantError && handlerCalls != before {
				t.Fatal("invalid arguments reached the tool handler")
			}
			if !strings.Contains(mcpResultText(result), tt.wantText) {
				t.Fatalf("result = %#v, want text containing %q", result.Content, tt.wantText)
			}
		})
	}
	if handlerCalls != 1 {
		t.Fatalf("handler calls = %d, want 1", handlerCalls)
	}
}

func TestMCPToolBuildArgsTypedOnly(t *testing.T) {
	tool := findMCPTool(t, "sheets_update_range")
	args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"spreadsheet_id": "sheet1",
			"range":          "Sheet1!A1:B1",
			"values_json":    `[[1,2]]`,
			"input":          "RAW",
			"args":           []any{"drive", "delete", "file"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(args, " ")
	if strings.Contains(got, "drive delete") {
		t.Fatalf("generic args leaked into typed tool argv: %#v", args)
	}
	want := []string{"sheets", "update", "--values-json", "[[1,2]]", "--input", "RAW", "--", "sheet1", "Sheet1!A1:B1"}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestMCPGmailSearchBuildArgs(t *testing.T) {
	tool := findMCPTool(t, "gmail_search")
	tests := []struct {
		name      string
		arguments map[string]any
		want      []string
	}{
		{
			name: "max and body",
			arguments: map[string]any{
				"query":        "from:person@example.com newer_than:7d",
				"max":          25,
				"include_body": true,
			},
			want: []string{
				"gmail", "messages", "search", "--max", "25", "--include-body", "--",
				"from:person@example.com newer_than:7d",
			},
		},
		{
			name: "default max without body",
			arguments: map[string]any{
				"query": "is:unread",
			},
			want: []string{"gmail", "messages", "search", "--max", "10", "--", "is:unread"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
				Arguments: tt.arguments,
			}})
			if err != nil {
				t.Fatal(err)
			}
			if got, want := strings.Join(args, "\x00"), strings.Join(tt.want, "\x00"); got != want {
				t.Fatalf("argv = %#v, want %#v", args, tt.want)
			}
		})
	}
}

func TestMCPGmailSearchInputSchemaRejectsUnsafeArguments(t *testing.T) {
	s := newMCPServer()
	handlerCalls := 0
	s.AddTool(newMCPTool(findMCPTool(t, "gmail_search")), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handlerCalls++
		return mcp.NewToolResultText("ok"), nil
	})

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
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "gog-test", Version: "1"}
	if _, err := client.Initialize(t.Context(), initRequest); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		arguments map[string]any
		wantText  string
	}{
		{
			name: "generic args",
			arguments: map[string]any{
				"query": "is:unread",
				"args":  []any{"gmail", "messages", "search"},
			},
			wantText: "args",
		},
		{
			name: "generic argv",
			arguments: map[string]any{
				"query": "is:unread",
				"argv":  []any{"gmail", "messages", "search"},
			},
			wantText: "argv",
		},
		{
			name: "missing query",
			arguments: map[string]any{
				"max": 10,
			},
			wantText: "query",
		},
		{
			name: "wrong type",
			arguments: map[string]any{
				"query": "is:unread",
				"max":   "10",
			},
			wantText: "max",
		},
		{
			name: "unknown field",
			arguments: map[string]any{
				"query": "is:unread",
				"extra": true,
			},
			wantText: "extra",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := handlerCalls
			result, err := client.CallTool(t.Context(), mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "gmail_search",
					Arguments: tt.arguments,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError {
				t.Fatalf("expected schema rejection, got %#v", result.Content)
			}
			if handlerCalls != before {
				t.Fatal("invalid arguments reached the tool handler")
			}
			if !strings.Contains(mcpResultText(result), tt.wantText) {
				t.Fatalf("result = %#v, want text containing %q", result.Content, tt.wantText)
			}
		})
	}
	if handlerCalls != 0 {
		t.Fatalf("handler calls = %d, want 0", handlerCalls)
	}
}

func TestMCPGmailGetMessageBuildArgs(t *testing.T) {
	tool := findMCPTool(t, "gmail_get_message")
	tests := []struct {
		name      string
		arguments map[string]any
		want      []string
		wantError string
	}{
		{
			name:      "message id is required",
			arguments: map[string]any{},
			wantError: "message_id",
		},
		{
			name: "sanitize content defaults to true",
			arguments: map[string]any{
				"message_id": "m1",
			},
			want: []string{"gmail", "get", "--sanitize-content", "--", "m1"},
		},
		{
			name: "sanitize content can be disabled",
			arguments: map[string]any{
				"message_id":       "m1",
				"sanitize_content": false,
			},
			want: []string{"gmail", "get", "--", "m1"},
		},
		{
			name: "message id is delimited from flags",
			arguments: map[string]any{
				"message_id": "--format=raw",
			},
			want: []string{"gmail", "get", "--sanitize-content", "--", "--format=raw"},
		},
		{
			name: "empty message id is rejected",
			arguments: map[string]any{
				"message_id": " \t",
			},
			wantError: "empty message_id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
				Arguments: tt.arguments,
			}})
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("BuildArgs error = %v, want %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildArgs: %v", err)
			}
			if strings.Join(args, "\x00") != strings.Join(tt.want, "\x00") {
				t.Fatalf("args = %#v, want %#v", args, tt.want)
			}
		})
	}
}

func TestMCPGmailGetMessageSchemaRejectsDownloads(t *testing.T) {
	tool := findMCPTool(t, "gmail_get_message")
	schemaTool := newMCPTool(tool)
	if len(schemaTool.InputSchema.Required) != 1 || schemaTool.InputSchema.Required[0] != "message_id" {
		t.Fatalf("required fields = %#v, want [message_id]", schemaTool.InputSchema.Required)
	}
	if len(schemaTool.InputSchema.Properties) != 2 {
		t.Fatalf("schema properties = %#v, want exactly message_id and sanitize_content", schemaTool.InputSchema.Properties)
	}
	for _, name := range []string{"download", "attachment_id", "output_dir", "args"} {
		if _, ok := schemaTool.InputSchema.Properties[name]; ok {
			t.Fatalf("schema exposes prohibited input %q", name)
		}
	}
	sanitizeSchema, ok := schemaTool.InputSchema.Properties["sanitize_content"].(map[string]any)
	if !ok || sanitizeSchema["type"] != "boolean" || sanitizeSchema["default"] != true {
		t.Fatalf("sanitize_content schema = %#v", schemaTool.InputSchema.Properties["sanitize_content"])
	}

	s := newMCPServer()
	handlerCalls := 0
	s.AddTool(schemaTool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handlerCalls++
		return mcp.NewToolResultText("ok"), nil
	})
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
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "gog-test", Version: "1"}
	if _, err := client.Initialize(t.Context(), initRequest); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		arguments map[string]any
		wantText  string
	}{
		{
			name: "download input",
			arguments: map[string]any{
				"message_id": "m1",
				"download":   true,
			},
			wantText: "download",
		},
		{
			name: "missing message id",
			arguments: map[string]any{
				"sanitize_content": true,
			},
			wantText: "message_id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := client.CallTool(t.Context(), mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "gmail_get_message",
					Arguments: tt.arguments,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError || !strings.Contains(mcpResultText(result), tt.wantText) {
				t.Fatalf("invalid input result = %#v, want schema error containing %q", result.Content, tt.wantText)
			}
		})
	}
	if handlerCalls != 0 {
		t.Fatal("schema-invalid input reached the tool handler")
	}
}

func TestMCPServerValidatesToolInputSchema(t *testing.T) {
	s := newMCPServer()
	handlerCalls := 0
	s.AddTool(newMCPTool(findMCPTool(t, "docs_write")), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handlerCalls++
		return mcp.NewToolResultText("ok"), nil
	})

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
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "gog-test", Version: "1"}
	if _, err := client.Initialize(t.Context(), initRequest); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		arguments map[string]any
		wantError bool
		wantText  string
	}{
		{
			name: "unknown field",
			arguments: map[string]any{
				"document_id": "doc1",
				"text":        "hello",
				"argv":        []any{"drive", "delete", "file"},
			},
			wantError: true,
			wantText:  "argv",
		},
		{
			name: "wrong type",
			arguments: map[string]any{
				"document_id": "doc1",
				"text":        "hello",
				"append":      "yes",
			},
			wantError: true,
			wantText:  "append",
		},
		{
			name: "missing required field",
			arguments: map[string]any{
				"text": "hello",
			},
			wantError: true,
			wantText:  "document_id",
		},
		{
			name: "valid",
			arguments: map[string]any{
				"document_id": "doc1",
				"text":        "hello",
				"append":      true,
			},
			wantText: "ok",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := handlerCalls
			result, err := client.CallTool(t.Context(), mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "docs_write",
					Arguments: tt.arguments,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError != tt.wantError {
				t.Fatalf("IsError = %v, want %v: %#v", result.IsError, tt.wantError, result.Content)
			}
			if tt.wantError && handlerCalls != before {
				t.Fatal("invalid arguments reached the tool handler")
			}
			if !strings.Contains(mcpResultText(result), tt.wantText) {
				t.Fatalf("result = %#v, want text containing %q", result.Content, tt.wantText)
			}
		})
	}
	if handlerCalls != 1 {
		t.Fatalf("handler calls = %d, want 1", handlerCalls)
	}
}

func TestMCPDocsWritePreservesTextWhitespace(t *testing.T) {
	tool := findMCPTool(t, "docs_write")
	args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"document_id": "doc1",
			"text":        "  indented\n",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for i, arg := range args {
		if arg == "--text" && i+1 < len(args) {
			if args[i+1] != "  indented\n" {
				t.Fatalf("text = %q", args[i+1])
			}
			return
		}
	}
	t.Fatalf("missing --text in %#v", args)
}

func TestMCPDocsWriteBuildArgsExact(t *testing.T) {
	tool := findMCPTool(t, "docs_write")
	tests := []struct {
		name      string
		arguments map[string]any
		want      []string
	}{
		{
			name: "append",
			arguments: map[string]any{
				"document_id": "doc1",
				"text":        "hello",
				"append":      true,
			},
			want: []string{"docs", "write", "--text", "hello", "--append", "--", "doc1"},
		},
		{
			name: "replace",
			arguments: map[string]any{
				"document_id": "doc1",
				"text":        "hello",
				"replace":     true,
			},
			want: []string{"docs", "write", "--text", "hello", "--replace", "--", "doc1"},
		},
		{
			name: "markdown",
			arguments: map[string]any{
				"document_id": "doc1",
				"text":        "# Heading",
				"append":      true,
				"markdown":    true,
			},
			want: []string{"docs", "write", "--text", "# Heading", "--append", "--markdown", "--", "doc1"},
		},
		{
			name: "tab",
			arguments: map[string]any{
				"document_id": "doc1",
				"text":        "hello",
				"append":      true,
				"tab":         "Second",
			},
			want: []string{"docs", "write", "--text", "hello", "--append", "--tab", "Second", "--", "doc1"},
		},
		{
			name: "leading-dash text",
			arguments: map[string]any{
				"document_id": "doc1",
				"text":        "-not-a-flag",
				"append":      true,
			},
			want: []string{"docs", "write", "--text", "-not-a-flag", "--append", "--", "doc1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
				Arguments: tt.arguments,
			}})
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(args, "\x00"); got != strings.Join(tt.want, "\x00") {
				t.Fatalf("args = %#v, want %#v", args, tt.want)
			}
		})
	}
}

func TestMCPDocsWriteRejectsNeitherAppendNorReplace(t *testing.T) {
	tool := findMCPTool(t, "docs_write")
	_, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"document_id": "doc1",
			"text":        "hello",
			"append":      false,
			"replace":     false,
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "append=false") {
		t.Fatalf("expected append=false error, got %v", err)
	}
}

func TestMCPDocsGetBuildArgs(t *testing.T) {
	tool := findMCPTool(t, "docs_get")
	tests := []struct {
		name      string
		arguments map[string]any
		want      []string
	}{
		{
			name:      "default max bytes and positional document ID",
			arguments: map[string]any{"document_id": "doc1"},
			want:      []string{"docs", "cat", "--max-bytes", "2000000", "--", "doc1"},
		},
		{
			name:      "zero max bytes",
			arguments: map[string]any{"document_id": "doc1", "max_bytes": 0},
			want:      []string{"docs", "cat", "--max-bytes", "0", "--", "doc1"},
		},
		{
			name:      "clamped max bytes",
			arguments: map[string]any{"document_id": "doc1", "max_bytes": 20_000_001},
			want:      []string{"docs", "cat", "--max-bytes", "20000000", "--", "doc1"},
		},
		{
			name:      "tab",
			arguments: map[string]any{"document_id": "doc1", "tab": "Overview"},
			want:      []string{"docs", "cat", "--max-bytes", "2000000", "--tab", "Overview", "--", "doc1"},
		},
		{
			name:      "all tabs",
			arguments: map[string]any{"document_id": "doc1", "all_tabs": true},
			want:      []string{"docs", "cat", "--max-bytes", "2000000", "--all-tabs", "--", "doc1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
				Arguments: tt.arguments,
			}})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(args, "\x00") != strings.Join(tt.want, "\x00") {
				t.Fatalf("args = %#v, want %#v", args, tt.want)
			}
		})
	}
}

func TestMCPDocsWriteRejectsExplicitEmptyTab(t *testing.T) {
	tool := findMCPTool(t, "docs_write")
	for _, tab := range []string{"", " \t "} {
		_, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"document_id": "doc1",
				"text":        "hello",
				"tab":         tab,
			},
		}})
		if err == nil || !strings.Contains(err.Error(), "tab cannot be empty") {
			t.Fatalf("tab %q error = %v, want empty-tab rejection", tab, err)
		}
	}
}

func TestMCPDocsGetRejectsTabWithAllTabs(t *testing.T) {
	tool := findMCPTool(t, "docs_get")
	_, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"document_id": "doc1",
			"tab":         "Overview",
			"all_tabs":    true,
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected tab/all_tabs error, got %v", err)
	}

	_, err = tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"document_id": "doc1",
			"tab":         "",
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "tab cannot be empty") {
		t.Fatalf("expected empty tab error, got %v", err)
	}
}

func TestMCPDriveGetBuildArgs(t *testing.T) {
	tool := findMCPTool(t, "drive_get")
	tests := []struct {
		name      string
		arguments map[string]any
		want      []string
		wantError string
	}{
		{
			name: "default fields preserve metadata mask",
			arguments: map[string]any{
				"file_id": "--file-id",
			},
			want: []string{"drive", "get", "--", "--file-id"},
		},
		{
			name: "optional fields before delimiter",
			arguments: map[string]any{
				"file_id": "--file-id",
				"fields":  "id,name,thumbnailLink",
			},
			want: []string{"drive", "get", "--fields", "id,name,thumbnailLink", "--", "--file-id"},
		},
		{
			name: "missing required file id",
			arguments: map[string]any{
				"fields": "id,name",
			},
			wantError: `required argument "file_id" not found`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
				Arguments: tt.arguments,
			}})
			if tt.wantError != "" {
				if err == nil || err.Error() != tt.wantError {
					t.Fatalf("error = %v, want %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(args, "\x00") != strings.Join(tt.want, "\x00") {
				t.Fatalf("args = %#v, want %#v", args, tt.want)
			}
		})
	}
}

func TestMCPDriveGetRejectsUnknownSchemaFields(t *testing.T) {
	s := newMCPServer()
	handlerCalls := 0
	s.AddTool(newMCPTool(findMCPTool(t, "drive_get")), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handlerCalls++
		return mcp.NewToolResultText("ok"), nil
	})

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
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "gog-test", Version: "1"}
	if _, err := client.Initialize(t.Context(), initRequest); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		arguments map[string]any
		wantError bool
		wantText  string
	}{
		{
			name: "unknown field",
			arguments: map[string]any{
				"file_id": "file1",
				"argv":    []any{"drive", "delete", "file1"},
			},
			wantError: true,
			wantText:  "argv",
		},
		{
			name:      "missing required field",
			arguments: map[string]any{},
			wantError: true,
			wantText:  "file_id",
		},
		{
			name: "valid",
			arguments: map[string]any{
				"file_id": "file1",
			},
			wantText: "ok",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := handlerCalls
			result, err := client.CallTool(t.Context(), mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "drive_get",
					Arguments: tt.arguments,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError != tt.wantError {
				t.Fatalf("IsError = %v, want %v: %#v", result.IsError, tt.wantError, result.Content)
			}
			if tt.wantError && handlerCalls != before {
				t.Fatal("invalid arguments reached the tool handler")
			}
			if !strings.Contains(mcpResultText(result), tt.wantText) {
				t.Fatalf("result = %#v, want text containing %q", result.Content, tt.wantText)
			}
		})
	}
	if handlerCalls != 1 {
		t.Fatalf("handler calls = %d, want 1", handlerCalls)
	}
}

func TestMCPSheetsUpdateRejectsFileExpansion(t *testing.T) {
	tool := findMCPTool(t, "sheets_update_range")
	_, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"spreadsheet_id": "sheet1",
			"range":          "Sheet1!A1",
			"values_json":    "@/tmp/secret.json",
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "literal JSON") {
		t.Fatalf("expected literal JSON error, got %v", err)
	}
}

func TestMCPSheetsUpdatePreservesLargeJSONNumbers(t *testing.T) {
	tool := findMCPTool(t, "sheets_update_range")
	args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"spreadsheet_id": "sheet1",
			"range":          "Sheet1!A1",
			"values_json":    `[[1234567890123456789]]`,
			"input":          "RAW",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for i, arg := range args {
		if arg == "--values-json" && i+1 < len(args) {
			if args[i+1] != `[[1234567890123456789]]` {
				t.Fatalf("values_json = %q", args[i+1])
			}
			return
		}
	}
	t.Fatalf("missing --values-json in %#v", args)
}

func TestMCPSheetsUpdateRejectsTrailingJSON(t *testing.T) {
	tool := findMCPTool(t, "sheets_update_range")
	_, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"spreadsheet_id": "sheet1",
			"range":          "Sheet1!A1",
			"values_json":    `[[1]] garbage`,
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "trailing content") {
		t.Fatalf("expected trailing content error, got %v", err)
	}
}

func TestMCPLimitedBufferCapsDuringWrite(t *testing.T) {
	buf := newMCPLimitedBuffer(5)
	n, err := buf.Write([]byte("hello world"))
	if err != nil {
		t.Fatal(err)
	}
	if n != len("hello world") {
		t.Fatalf("Write returned %d", n)
	}
	got := buf.String()
	if !strings.HasPrefix(got, "hello") || !strings.Contains(got, "truncated") {
		t.Fatalf("unexpected buffer: %q", got)
	}
}

func TestMCPCalendarEventsBuildArgs(t *testing.T) {
	tool := findMCPTool(t, "calendar_events")
	tests := []struct {
		name      string
		arguments map[string]any
		want      []string
	}{
		{
			name:      "defaults",
			arguments: map[string]any{},
			want:      []string{"calendar", "events", "--max", "10"},
		},
		{
			name: "calendar ID",
			arguments: map[string]any{
				"calendar_id": "family@example.com",
			},
			want: []string{"calendar", "events", "--max", "10", "--", "family@example.com"},
		},
		{
			name: "ranges",
			arguments: map[string]any{
				"from": "2026-08-04T09:00:00Z",
				"to":   "2026-08-04T17:00:00Z",
			},
			want: []string{
				"calendar", "events",
				"--from", "2026-08-04T09:00:00Z",
				"--to", "2026-08-04T17:00:00Z",
				"--max", "10",
			},
		},
		{
			name:      "today",
			arguments: map[string]any{"today": true},
			want:      []string{"calendar", "events", "--today", "--max", "10"},
		},
		{
			name:      "tomorrow",
			arguments: map[string]any{"tomorrow": true},
			want:      []string{"calendar", "events", "--tomorrow", "--max", "10"},
		},
		{
			name:      "days",
			arguments: map[string]any{"days": 3},
			want:      []string{"calendar", "events", "--days", "3", "--max", "10"},
		},
		{
			name:      "max",
			arguments: map[string]any{"max": 42},
			want:      []string{"calendar", "events", "--max", "42"},
		},
		{
			name:      "page token is trimmed and follows max",
			arguments: map[string]any{"max": 42, "page_token": " page-2 "},
			want:      []string{"calendar", "events", "--max", "42", "--page=page-2"},
		},
		{
			name:      "leading dash page token stays opaque",
			arguments: map[string]any{"page_token": "-opaque"},
			want:      []string{"calendar", "events", "--max", "10", "--page=-opaque"},
		},
		{
			name:      "all pages uses existing CLI flag",
			arguments: map[string]any{"all_pages": true, "calendar_id": "family@example.com"},
			want:      []string{"calendar", "events", "--max", "10", "--all-pages", "--", "family@example.com"},
		},
		{
			name:      "query",
			arguments: map[string]any{"query": "team planning"},
			want:      []string{"calendar", "events", "--query", "team planning", "--max", "10"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
				Arguments: tt.arguments,
			}})
			if err != nil {
				t.Fatalf("BuildArgs: %v", err)
			}
			if strings.Join(args, "\x00") != strings.Join(tt.want, "\x00") {
				t.Fatalf("args = %#v, want %#v", args, tt.want)
			}
			for _, arg := range args {
				if arg == "--json" {
					t.Fatalf("adapter supplied runner-owned JSON flag: %#v", args)
				}
			}
		})
	}
}

func TestMCPCalendarEventsBuildArgsRejectsPagingConflictsAndEmptyTokens(t *testing.T) {
	tool := findMCPTool(t, "calendar_events")
	tests := []struct {
		name      string
		arguments map[string]any
		wantText  string
	}{
		{
			name:      "page token and all pages conflict",
			arguments: map[string]any{"page_token": "next", "all_pages": true},
			wantText:  "cannot be combined",
		},
		{
			name:      "explicit empty page token",
			arguments: map[string]any{"page_token": "   "},
			wantText:  "must not be empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
				Arguments: tt.arguments,
			}})
			if err == nil || !strings.Contains(err.Error(), tt.wantText) {
				t.Fatalf("BuildArgs error = %v, want text %q", err, tt.wantText)
			}
		})
	}
}

func TestMCPCalendarEventsSchemaIsClosedAndTyped(t *testing.T) {
	s := newMCPServer()
	handlerCalls := 0
	s.AddTool(newMCPTool(findMCPTool(t, "calendar_events")), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handlerCalls++
		return mcp.NewToolResultText("ok"), nil
	})

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
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "gog-test", Version: "1"}
	if _, err := client.Initialize(t.Context(), initRequest); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		arguments map[string]any
		wantError bool
		wantText  string
	}{
		{
			name: "unknown field",
			arguments: map[string]any{
				"page": "next",
			},
			wantError: true,
			wantText:  "page",
		},
		{
			name: "unknown multi-calendar field",
			arguments: map[string]any{
				"calendars": []any{"primary", "family"},
			},
			wantError: true,
			wantText:  "calendars",
		},
		{
			name: "unknown all-calendars selector",
			arguments: map[string]any{
				"all": true,
			},
			wantError: true,
			wantText:  "all",
		},
		{
			name: "unknown repeatable calendar selector",
			arguments: map[string]any{
				"cal": []any{"primary"},
			},
			wantError: true,
			wantText:  "cal",
		},
		{
			name: "unknown event-type filter",
			arguments: map[string]any{
				"event_types": []any{"default"},
			},
			wantError: true,
			wantText:  "event_types",
		},
		{
			name: "unknown singular event-type filter",
			arguments: map[string]any{
				"event_type": "default",
			},
			wantError: true,
			wantText:  "event_type",
		},
		{
			name: "unknown sorting control",
			arguments: map[string]any{
				"sort": "start",
			},
			wantError: true,
			wantText:  "sort",
		},
		{
			name: "unknown order control",
			arguments: map[string]any{
				"order": "asc",
			},
			wantError: true,
			wantText:  "order",
		},
		{
			name: "unknown field-mask control",
			arguments: map[string]any{
				"fields": "items(id)",
			},
			wantError: true,
			wantText:  "fields",
		},
		{
			name: "wrong type",
			arguments: map[string]any{
				"max": "25",
			},
			wantError: true,
			wantText:  "max",
		},
		{
			name: "wrong page token type",
			arguments: map[string]any{
				"page_token": 2,
			},
			wantError: true,
			wantText:  "page_token",
		},
		{
			name: "wrong all pages type",
			arguments: map[string]any{
				"all_pages": "true",
			},
			wantError: true,
			wantText:  "all_pages",
		},
		{
			name: "all supported fields",
			arguments: map[string]any{
				"calendar_id": "family@example.com",
				"from":        "2026-08-04",
				"to":          "2026-08-05",
				"today":       false,
				"tomorrow":    true,
				"days":        3,
				"max":         25,
				"page_token":  "next",
				"all_pages":   false,
			},
			wantText: "ok",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := handlerCalls
			result, err := client.CallTool(t.Context(), mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "calendar_events",
					Arguments: tt.arguments,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError != tt.wantError {
				t.Fatalf("IsError = %v, want %v: %#v", result.IsError, tt.wantError, result.Content)
			}
			if tt.wantError && handlerCalls != before {
				t.Fatal("invalid arguments reached the tool handler")
			}
			if !strings.Contains(mcpResultText(result), tt.wantText) {
				t.Fatalf("result = %#v, want text containing %q", result.Content, tt.wantText)
			}
		})
	}
	if handlerCalls != 1 {
		t.Fatalf("handler calls = %d, want 1", handlerCalls)
	}
}

func TestMCPCalendarEventsRunnerSuppliesJSON(t *testing.T) {
	tool := findMCPTool(t, "calendar_events")
	args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{"calendar_id": "primary"},
	}})
	if err != nil {
		t.Fatalf("BuildArgs: %v", err)
	}
	for _, arg := range args {
		if arg == "--json" {
			t.Fatalf("adapter supplied runner-owned JSON flag: %#v", args)
		}
	}
	hasJSON := false
	for _, arg := range mcpParentRootArgs(nil) {
		if arg == "--json" {
			hasJSON = true
			break
		}
	}
	if !hasJSON {
		t.Fatalf("runner root args missing --json: %#v", mcpParentRootArgs(nil))
	}

	t.Setenv("GOG_MCP_M08_RUNNER_HELPER", "1")
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self:           os.Args[0],
		tool:           tool,
		commandArgs:    []string{"-test.run=TestMCPM08RunnerHelper$"},
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
	if got.ExitCode != 0 {
		t.Fatalf("runner exit code = %d, stderr = %q", got.ExitCode, got.Stderr)
	}
	stdout, ok := got.Stdout.(map[string]any)
	if !ok {
		t.Fatalf("runner stdout type = %T, want JSON object", got.Stdout)
	}
	if _, ok := stdout["events"]; !ok {
		t.Fatalf("runner stdout = %#v, want events field", stdout)
	}
}

func TestMCPCalendarEventsRunnerBoundsLargeOutput(t *testing.T) {
	const maxOutputBytes = 128
	tool := findMCPTool(t, "calendar_events")
	args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{"all_pages": true},
	}})
	if err != nil {
		t.Fatalf("BuildArgs: %v", err)
	}
	if !slices.Contains(args, "--all-pages") {
		t.Fatalf("all_pages args = %#v, want --all-pages", args)
	}

	t.Setenv("GOG_MCP_M08_RUNNER_HELPER", "1")
	t.Setenv("GOG_MCP_M12_RUNNER_MODE", "large")
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self:           os.Args[0],
		tool:           tool,
		commandArgs:    []string{"-test.run=TestMCPM08RunnerHelper$"},
		timeout:        5 * time.Second,
		maxOutputBytes: maxOutputBytes,
	})
	if result.IsError {
		t.Fatalf("runner result is error: %#v", result.Content)
	}
	got, ok := result.StructuredContent.(mcpCommandResult)
	if !ok {
		t.Fatalf("structured result type = %T, want mcpCommandResult", result.StructuredContent)
	}
	if got.ExitCode != 0 || got.Stderr != "" {
		t.Fatalf("bounded runner result = %#v", got)
	}
	stdout, ok := got.Stdout.(string)
	if !ok {
		t.Fatalf("bounded stdout type = %T, want string", got.Stdout)
	}
	if !strings.Contains(stdout, "... [output truncated]") {
		t.Fatalf("bounded stdout = %q, want truncation marker", stdout)
	}
	if len(stdout) > maxOutputBytes+len("\n... [output truncated]") {
		t.Fatalf("bounded stdout length = %d, exceeds cap %d plus marker", len(stdout), maxOutputBytes)
	}
}

func TestMCPM08RunnerHelper(t *testing.T) {
	if os.Getenv("GOG_MCP_M08_RUNNER_HELPER") != "1" {
		return
	}
	if os.Getenv("GOG_MCP_M12_RUNNER_MODE") == "large" {
		_, _ = io.WriteString(os.Stdout, `{"events":[`+strings.Repeat(`{"id":"large"},`, 256)+`{"id":"large"}]}`)
		os.Exit(0)
	}
	_, _ = io.WriteString(os.Stdout, `{"events":[]}`)
	os.Exit(0)
}

func hasMCPTool(tools []mcpToolSpec, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func toolNames(tools []mcpToolSpec) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Name)
	}
	return out
}

func findMCPTool(t *testing.T, name string) mcpToolSpec {
	t.Helper()
	for _, tool := range mcpAllTools() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("missing tool %s", name)
	return mcpToolSpec{}
}

func mcpResultText(result *mcp.CallToolResult) string {
	var text strings.Builder
	for _, content := range result.Content {
		if item, ok := content.(mcp.TextContent); ok {
			text.WriteString(item.Text)
		}
	}
	return text.String()
}

func TestMCPSheetsReadRangeBuildArgs(t *testing.T) {
	tool := findMCPTool(t, "sheets_read_range")
	tests := []struct {
		name      string
		arguments map[string]any
		want      []string
	}{
		{
			name: "default render",
			arguments: map[string]any{
				"spreadsheet_id": "sheet1",
				"range":          "Sheet1!A1:B2",
			},
			want: []string{"sheets", "get", "--", "sheet1", "Sheet1!A1:B2"},
		},
		{
			name: "rows dimension",
			arguments: map[string]any{
				"spreadsheet_id": "sheet1",
				"range":          "Sheet1!A1:B2",
				"dimension":      "ROWS",
			},
			want: []string{"sheets", "get", "--dimension", "ROWS", "--", "sheet1", "Sheet1!A1:B2"},
		},
		{
			name: "columns dimension with formula render",
			arguments: map[string]any{
				"spreadsheet_id": "sheet1",
				"range":          "Sheet1!A1:B2",
				"dimension":      "COLUMNS",
				"render":         "FORMULA",
			},
			want: []string{"sheets", "get", "--dimension", "COLUMNS", "--render", "FORMULA", "--", "sheet1", "Sheet1!A1:B2"},
		},
		{
			name: "formatted render",
			arguments: map[string]any{
				"spreadsheet_id": "sheet1",
				"range":          "Sheet1!A1:B2",
				"render":         "FORMATTED_VALUE",
			},
			want: []string{"sheets", "get", "--render", "FORMATTED_VALUE", "--", "sheet1", "Sheet1!A1:B2"},
		},
		{
			name: "unformatted render",
			arguments: map[string]any{
				"spreadsheet_id": "sheet1",
				"range":          "Sheet1!A1:B2",
				"render":         "UNFORMATTED_VALUE",
			},
			want: []string{"sheets", "get", "--render", "UNFORMATTED_VALUE", "--", "sheet1", "Sheet1!A1:B2"},
		},
		{
			name: "formula render",
			arguments: map[string]any{
				"spreadsheet_id": "sheet1",
				"range":          "Sheet1!A1:B2",
				"render":         "FORMULA",
			},
			want: []string{"sheets", "get", "--render", "FORMULA", "--", "sheet1", "Sheet1!A1:B2"},
		},
		{
			name: "separator before positional values",
			arguments: map[string]any{
				"spreadsheet_id": "-sheet1",
				"range":          "-Sheet1!A1",
			},
			want: []string{"sheets", "get", "--", "-sheet1", "-Sheet1!A1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
				Arguments: tt.arguments,
			}})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(args, "\x00") != strings.Join(tt.want, "\x00") {
				t.Fatalf("args = %#v, want %#v", args, tt.want)
			}
		})
	}
}

func TestMCPSheetsReadRangeRequiresSpreadsheetAndRange(t *testing.T) {
	tool := findMCPTool(t, "sheets_read_range")
	tests := []struct {
		name      string
		arguments map[string]any
		wantError string
	}{
		{
			name: "missing spreadsheet ID",
			arguments: map[string]any{
				"range": "Sheet1!A1",
			},
			wantError: `required argument "spreadsheet_id"`,
		},
		{
			name: "missing range",
			arguments: map[string]any{
				"spreadsheet_id": "sheet1",
			},
			wantError: `required argument "range"`,
		},
		{
			name: "empty spreadsheet ID",
			arguments: map[string]any{
				"spreadsheet_id": "",
				"range":          "Sheet1!A1",
			},
			wantError: "empty spreadsheet_id",
		},
		{
			name: "empty range",
			arguments: map[string]any{
				"spreadsheet_id": "sheet1",
				"range":          " ",
			},
			wantError: "empty range",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
				Arguments: tt.arguments,
			}})
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want text %q", err, tt.wantError)
			}
		})
	}
}

func TestMCPServerValidatesSheetsReadRangeInputSchema(t *testing.T) {
	s := newMCPServer()
	handlerCalls := 0
	s.AddTool(newMCPTool(findMCPTool(t, "sheets_read_range")), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handlerCalls++
		return mcp.NewToolResultText("ok"), nil
	})

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
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "gog-test", Version: "1"}
	if _, err := client.Initialize(t.Context(), initRequest); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		arguments map[string]any
		wantError bool
		wantText  string
	}{
		{
			name: "missing spreadsheet ID",
			arguments: map[string]any{
				"range": "Sheet1!A1",
			},
			wantError: true,
			wantText:  "spreadsheet_id",
		},
		{
			name: "missing range",
			arguments: map[string]any{
				"spreadsheet_id": "sheet1",
			},
			wantError: true,
			wantText:  "range",
		},
		{
			name: "wrong spreadsheet ID type",
			arguments: map[string]any{
				"spreadsheet_id": 123,
				"range":          "Sheet1!A1",
			},
			wantError: true,
			wantText:  "spreadsheet_id",
		},
		{
			name: "wrong range type",
			arguments: map[string]any{
				"spreadsheet_id": "sheet1",
				"range":          123,
			},
			wantError: true,
			wantText:  "range",
		},
		{
			name: "invalid render enum",
			arguments: map[string]any{
				"spreadsheet_id": "sheet1",
				"range":          "Sheet1!A1",
				"render":         "INVALID",
			},
			wantError: true,
			wantText:  "render",
		},
		{
			name: "invalid dimension enum",
			arguments: map[string]any{
				"spreadsheet_id": "sheet1",
				"range":          "Sheet1!A1",
				"dimension":      "INVALID",
			},
			wantError: true,
			wantText:  "dimension",
		},
		{
			name: "wrong dimension type",
			arguments: map[string]any{
				"spreadsheet_id": "sheet1",
				"range":          "Sheet1!A1",
				"dimension":      123,
			},
			wantError: true,
			wantText:  "dimension",
		},
		{
			name: "empty dimension",
			arguments: map[string]any{
				"spreadsheet_id": "sheet1",
				"range":          "Sheet1!A1",
				"dimension":      "",
			},
			wantError: true,
			wantText:  "dimension",
		},
		{
			name: "unknown field",
			arguments: map[string]any{
				"spreadsheet_id": "sheet1",
				"range":          "Sheet1!A1",
				"unexpected":     true,
			},
			wantError: true,
			wantText:  "unexpected",
		},
		{
			name: "valid",
			arguments: map[string]any{
				"spreadsheet_id": "sheet1",
				"range":          "Sheet1!A1",
				"dimension":      "COLUMNS",
				"render":         "FORMULA",
			},
			wantText: "ok",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := handlerCalls
			result, err := client.CallTool(t.Context(), mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      "sheets_read_range",
					Arguments: tt.arguments,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError != tt.wantError {
				t.Fatalf("IsError = %v, want %v: %#v", result.IsError, tt.wantError, result.Content)
			}
			if tt.wantError && handlerCalls != before {
				t.Fatal("invalid arguments reached the tool handler")
			}
			if !strings.Contains(mcpResultText(result), tt.wantText) {
				t.Fatalf("result = %#v, want text containing %q", result.Content, tt.wantText)
			}
		})
	}
	if handlerCalls != 1 {
		t.Fatalf("handler calls = %d, want 1", handlerCalls)
	}
}

func TestMCPSheetsReadRangeRunnerReturnsStructuredResult(t *testing.T) {
	tool := findMCPTool(t, "sheets_read_range")
	args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{
			"spreadsheet_id": "sheet1",
			"range":          "Sheet1!A1:B2",
			"dimension":      "COLUMNS",
		},
	}})
	if err != nil {
		t.Fatalf("BuildArgs: %v", err)
	}
	wantArgs := []string{"sheets", "get", "--dimension", "COLUMNS", "--", "sheet1", "Sheet1!A1:B2"}
	if strings.Join(args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}

	t.Setenv("GOG_MCP_M13_RUNNER_HELPER", "1")
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self:           os.Args[0],
		tool:           tool,
		commandArgs:    []string{"-test.run=TestMCPM13RunnerHelper$"},
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
	if got.Tool != "sheets_read_range" || got.Service != "sheets" || got.Risk != string(mcpRiskRead) {
		t.Fatalf("structured metadata = %#v", got)
	}
	if got.ExitCode != 0 {
		t.Fatalf("runner exit code = %d, stderr = %q", got.ExitCode, got.Stderr)
	}
	stdout, ok := got.Stdout.(map[string]any)
	if !ok {
		t.Fatalf("runner stdout type = %T, want JSON object", got.Stdout)
	}
	if stdout["range"] != "Sheet1!A1:B2" {
		t.Fatalf("runner range = %#v", stdout["range"])
	}
	values, ok := stdout["values"].([]any)
	if !ok || len(values) != 1 {
		t.Fatalf("runner values = %#v, want one row", stdout["values"])
	}
	row, ok := values[0].([]any)
	if !ok || len(row) != 2 || row[0] != "status" || row[1] != "ready" {
		t.Fatalf("runner first row = %#v", values[0])
	}
	if got.Stderr != "" {
		t.Fatalf("runner stderr = %q, want empty", got.Stderr)
	}
}

func TestMCPM13RunnerHelper(t *testing.T) {
	if os.Getenv("GOG_MCP_M13_RUNNER_HELPER") != "1" {
		return
	}
	_, _ = io.WriteString(os.Stdout, `{"range":"Sheet1!A1:B2","values":[["status","ready"]]}`)
	os.Exit(0)
}

func TestMCPWaveAAdaptersBuildExactArgsAndPolicy(t *testing.T) {
	tests := []struct {
		name      string
		risk      mcpToolRisk
		arguments map[string]any
		want      []string
	}{
		{"gmail_list_labels", mcpRiskRead, nil, []string{"gmail", "labels", "list"}},
		{"gmail_list_drafts", mcpRiskRead, nil, []string{"gmail", "drafts", "list", "--max", "20"}},
		{"gmail_get_draft", mcpRiskRead, map[string]any{"draft_id": "d1"}, []string{"gmail", "drafts", "get", "--", "d1"}},
		{"gmail_create_draft", mcpRiskWrite, map[string]any{"subject": "subject", "body": "body"}, []string{"gmail", "drafts", "create", "--subject", "subject", "--body", "body"}},
		{"gmail_modify_message_labels", mcpRiskWrite, map[string]any{"message_id": "m1", "add": "STARRED"}, []string{"gmail", "messages", "modify", "--add", "STARRED", "--", "m1"}},
		{"gmail_modify_thread_labels", mcpRiskWrite, map[string]any{"thread_id": "t1", "remove": "INBOX"}, []string{"gmail", "thread", "modify", "--remove", "INBOX", "--", "t1"}},
		{"gmail_archive_messages", mcpRiskWrite, map[string]any{"message_ids": []string{"m1", "m2"}}, []string{"gmail", "archive", "--", "m1", "m2"}},
		{"gmail_archive_threads", mcpRiskWrite, map[string]any{"thread_ids": []string{"t1", "t2"}}, []string{"gmail", "archive", "--thread", "--", "t1", "t2"}},
		{"gmail_mark_messages_read", mcpRiskWrite, map[string]any{"message_ids": []string{"m1"}}, []string{"gmail", "mark-read", "--", "m1"}},
		{"gmail_mark_messages_unread", mcpRiskWrite, map[string]any{"message_ids": []string{"m1"}}, []string{"gmail", "unread", "--", "m1"}},
		{"calendar_list_calendars", mcpRiskRead, nil, []string{"calendar", "calendars", "--max", "100"}},
		{"calendar_search_events", mcpRiskRead, map[string]any{"query": "planning"}, []string{"calendar", "search", "--max", "10", "--", "planning"}},
		{"calendar_get_event", mcpRiskRead, map[string]any{"calendar_id": "primary", "event_id": "e1"}, []string{"calendar", "event", "--", "primary", "e1"}},
		{"calendar_freebusy", mcpRiskRead, map[string]any{"from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z"}, []string{"calendar", "freebusy", "--from", "2026-08-04T09:00:00Z", "--to", "2026-08-04T10:00:00Z"}},
		{"calendar_find_conflicts", mcpRiskRead, map[string]any{"days": 1, "all": true}, []string{"calendar", "conflicts", "--days", "1", "--all"}},
		{"calendar_create_event", mcpRiskWrite, map[string]any{"calendar_id": "primary", "summary": "Plan", "from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z"}, []string{"calendar", "create", "--summary", "Plan", "--from", "2026-08-04T09:00:00Z", "--to", "2026-08-04T10:00:00Z", "--send-updates", "none", "--", "primary"}},
		{"calendar_respond_to_event", mcpRiskWrite, map[string]any{"calendar_id": "primary", "event_id": "e1", "status": "accepted"}, []string{"calendar", "respond", "--status", "accepted", "--", "primary", "e1"}},
		{"calendar_move_event", mcpRiskWrite, map[string]any{"source_calendar_id": "source", "event_id": "e1", "destination_calendar_id": "destination"}, []string{"calendar", "move", "--send-updates", "none", "--", "source", "e1", "destination"}},
		{"calendar_create_calendar", mcpRiskWrite, map[string]any{"summary": "Team"}, []string{"calendar", "create-calendar", "--", "Team"}},
		{"calendar_subscribe", mcpRiskWrite, map[string]any{"calendar_id": "raw@example.com"}, []string{"calendar", "subscribe", "--", "raw@example.com"}},
		{"calendar_unsubscribe", mcpRiskWrite, map[string]any{"calendar_id": "team"}, []string{"calendar", "unsubscribe", "--force", "--", "team"}},
		{"calendar_focus_time", mcpRiskWrite, map[string]any{"from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z"}, []string{"calendar", "focus-time", "--summary", "Focus Time", "--from", "2026-08-04T09:00:00Z", "--to", "2026-08-04T10:00:00Z", "--auto-decline", "all", "--chat-status", "doNotDisturb", "--", "primary"}},
		{"calendar_out_of_office", mcpRiskWrite, map[string]any{"from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z"}, []string{"calendar", "out-of-office", "--summary", "Out of office", "--from", "2026-08-04T09:00:00Z", "--to", "2026-08-04T10:00:00Z", "--auto-decline", "all", "--decline-message", "I am out of office and will respond when I return.", "--", "primary"}},
		{"calendar_working_location", mcpRiskWrite, map[string]any{"from": "2026-08-04", "to": "2026-08-05", "type": "home"}, []string{"calendar", "working-location", "--type", "home", "--from", "2026-08-04", "--to", "2026-08-05", "--", "primary"}},
		{"drive_permissions", mcpRiskRead, map[string]any{"file_id": "f1"}, []string{"drive", "permissions", "--max", "100", "--", "f1"}},
		{"drive_create_folder", mcpRiskWrite, map[string]any{"name": "Folder", "parent": "p1"}, []string{"drive", "mkdir", "Folder", "--parent", "p1"}},
		{"drive_rename", mcpRiskWrite, map[string]any{"file_id": "f1", "new_name": "Renamed"}, []string{"drive", "rename", "--", "f1", "Renamed"}},
		{"drive_move", mcpRiskWrite, map[string]any{"file_id": "f1", "destination_parent": "p1"}, []string{"drive", "move", "--parent", "p1", "--", "f1"}},
		{"drive_copy", mcpRiskWrite, map[string]any{"source_id": "f1", "new_name": "Copy", "parent": "p1"}, []string{"drive", "copy", "--parent", "p1", "--", "f1", "Copy"}},
		{"drive_create_shortcut", mcpRiskWrite, map[string]any{"target_id": "f1", "parent_id": "p1", "name": "Shortcut"}, []string{"drive", "shortcut", "create", "--parent", "p1", "--name", "Shortcut", "--", "f1"}},
		{"drive_create_comment", mcpRiskWrite, map[string]any{"file_id": "f1", "content": "Comment", "quoted_text": "Quote"}, []string{"drive", "comments", "create", "f1", "Comment", "--quoted", "Quote"}},
		{"docs_create", mcpRiskWrite, map[string]any{"title": "Doc", "parent": "p1", "pageless": true}, []string{"docs", "create", "--parent", "p1", "--pageless", "--", "Doc"}},
		{"sheets_create", mcpRiskWrite, map[string]any{"title": "Sheet", "sheet_names": []string{"One", "Two"}, "parent": "p1"}, []string{"sheets", "create", "Sheet", "--sheets", "One,Two", "--parent", "p1"}},
		{"slides_create_from_template", mcpRiskWrite, map[string]any{"template_id": "tpl", "title": "Deck", "replacements": []string{"name=Ryan"}, "parent": "p1", "exact": true}, []string{"slides", "create-from-template", "tpl", "Deck", "--replace", "name=Ryan", "--parent", "p1", "--exact"}},
	}

	seen := make(map[string]struct{}, len(tests))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, duplicate := seen[tt.name]; duplicate {
				t.Fatalf("duplicate test contract for %s", tt.name)
			}
			seen[tt.name] = struct{}{}
			tool := findMCPTool(t, tt.name)
			if tool.Risk != tt.risk {
				t.Fatalf("risk = %q, want %q", tool.Risk, tt.risk)
			}
			args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tt.arguments}})
			if err != nil {
				t.Fatalf("BuildArgs: %v", err)
			}
			if got, want := strings.Join(args, "\x00"), strings.Join(tt.want, "\x00"); got != want {
				t.Fatalf("args = %#v, want %#v", args, tt.want)
			}
			if tt.risk == mcpRiskWrite {
				if hasMCPTool(mcpEnabledTools(McpCmd{}), tt.name) {
					t.Fatalf("write tool %q exposed by default", tt.name)
				}
				if !hasMCPTool(mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{tt.name}}), tt.name) {
					t.Fatalf("write tool %q not exposed by exact authorized selector", tt.name)
				}
				for _, selector := range []string{tool.Service, tool.Service + ".*", "write", "all"} {
					if !hasMCPTool(mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{selector}}), tt.name) {
						t.Errorf("write tool %q not exposed by authorized selector %q", tt.name, selector)
					}
				}
			}
		})
	}
}

func TestMCPWaveARegistryNamesAreUniqueAndSchemasClosed(t *testing.T) {
	seen := make(map[string]struct{})
	for _, spec := range mcpAllTools() {
		if _, duplicate := seen[spec.Name]; duplicate {
			t.Fatalf("duplicate MCP tool name %q", spec.Name)
		}
		seen[spec.Name] = struct{}{}
		tool := newMCPTool(spec)
		closed, ok := tool.InputSchema.AdditionalProperties.(bool)
		if !ok || closed {
			t.Fatalf("tool %q input schema is not closed", spec.Name)
		}
	}
}

func TestMCPWaveACanonicalSchemaFields(t *testing.T) {
	tests := []struct {
		tool    string
		present []string
		absent  []string
	}{
		{"calendar_events", []string{"page_token", "all_pages"}, []string{"page", "all"}},
		{"calendar_move_event", []string{"source_calendar_id", "event_id", "destination_calendar_id"}, nil},
		{"drive_move", []string{"file_id", "destination_parent"}, []string{"parent"}},
		{"drive_copy", []string{"source_id", "new_name", "parent"}, []string{"file_id", "name"}},
		{"drive_create_shortcut", []string{"target_id", "parent_id", "name"}, []string{"parent"}},
		{"drive_create_comment", []string{"file_id", "content", "quoted_text"}, []string{"quoted", "anchor"}},
		{"sheets_create", []string{"title", "sheet_names", "parent"}, []string{"sheets"}},
		{"slides_create_from_template", []string{"template_id", "title", "replacements", "parent", "exact"}, []string{"file", "replacements_file", "markdown", "dry_run"}},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			properties := newMCPTool(findMCPTool(t, tt.tool)).InputSchema.Properties
			for _, field := range tt.present {
				if _, ok := properties[field]; !ok {
					t.Errorf("canonical field %q missing from %#v", field, properties)
				}
			}
			for _, field := range tt.absent {
				if _, ok := properties[field]; ok {
					t.Errorf("noncanonical or prohibited field %q exposed", field)
				}
			}
		})
	}
}

func TestMCPWaveACalendarValidationRejectsUnsafeDirectCalls(t *testing.T) {
	tests := []struct {
		name      string
		arguments map[string]any
	}{
		{"calendar_create_event", map[string]any{"calendar_id": "primary", "summary": "Plan", "from": "tomorrow", "to": "later"}},
		{"calendar_create_event", map[string]any{"calendar_id": "primary", "summary": "Plan", "from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z", "timezone": "Not/AZone"}},
		{"calendar_create_event", map[string]any{"calendar_id": "primary", "summary": "Plan", "from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z", "send_updates": "notifyEveryone"}},
		{"calendar_subscribe", map[string]any{"calendar_id": "raw@example.com", "color_id": "25"}},
		{"calendar_focus_time", map[string]any{"from": "tomorrow", "to": "later"}},
		{"calendar_focus_time", map[string]any{"from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z", "chat_status": "busy"}},
		{"calendar_out_of_office", map[string]any{"from": "2026-08-04", "to": "2026-08-05"}},
		{"calendar_out_of_office", map[string]any{"from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z", "calendar_id": " "}},
		{"calendar_working_location", map[string]any{"from": "2026-08-04", "to": "2026-08-05", "type": "home", "building_id": "building"}},
		{"calendar_working_location", map[string]any{"from": "2026-08-04", "to": "2026-08-05", "type": "custom"}},
		{"calendar_working_location", map[string]any{"from": "2026-08-04", "to": "2026-08-05", "type": "somewhere"}},
		{"calendar_working_location", map[string]any{"from": "2026-08-04", "to": "2026-08-05", "type": "home", "calendar_id": " "}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := findMCPTool(t, tt.name).BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tt.arguments}})
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestMCPWaveAArrayBoundaries(t *testing.T) {
	ids := make([]string, 1000)
	for i := range ids {
		ids[i] = "message-id"
	}
	overflowIDs := make([]string, 1001)
	copy(overflowIDs, ids)
	overflowIDs[len(overflowIDs)-1] = "overflow"
	for _, toolName := range []string{"gmail_mark_messages_read", "gmail_mark_messages_unread"} {
		t.Run(toolName, func(t *testing.T) {
			tool := findMCPTool(t, toolName)
			if _, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
				Arguments: map[string]any{"message_ids": ids},
			}}); err != nil {
				t.Fatalf("1,000 IDs: %v", err)
			}
			if _, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
				Arguments: map[string]any{"message_ids": overflowIDs},
			}}); err == nil {
				t.Fatal("expected 1,001 IDs to be rejected")
			}
			if _, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
				Arguments: map[string]any{"message_ids": []string{}},
			}}); err == nil {
				t.Fatal("expected empty ID list to be rejected")
			}
		})
	}

	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = "value"
	}
	for _, test := range []struct {
		tool  string
		field string
		args  map[string]any
	}{
		{"calendar_freebusy", "extra_calendar_ids", map[string]any{"from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z"}},
		{"calendar_find_conflicts", "calendar_ids", map[string]any{"days": 1}},
		{"calendar_create_event", "attendees", map[string]any{"calendar_id": "primary", "summary": "Plan", "from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z"}},
		{"calendar_create_event", "rrule", map[string]any{"calendar_id": "primary", "summary": "Plan", "from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z"}},
		{"calendar_focus_time", "rrule", map[string]any{"from": "2026-08-04T09:00:00Z", "to": "2026-08-04T10:00:00Z"}},
	} {
		t.Run(test.tool+"/"+test.field, func(t *testing.T) {
			test.args[test.field] = tooMany
			if _, err := findMCPTool(t, test.tool).BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
				Arguments: test.args,
			}}); err == nil {
				t.Fatal("expected 101-item array to be rejected")
			}
		})
	}

	if _, err := findMCPTool(t, "slides_create_from_template").BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{"template_id": "tpl", "title": "Deck"},
	}}); err == nil {
		t.Fatal("expected missing replacements to be rejected before child execution")
	}
}

func TestMCPE03DriveExclusionsAcrossSelectorsSchemasAndArgv(t *testing.T) {
	selectors := []McpCmd{
		{},
		{AllowWrite: true, AllowTool: []string{"write"}},
		{AllowWrite: true, AllowTool: []string{"drive"}},
		{AllowWrite: true, AllowTool: []string{"drive.*"}},
		{AllowWrite: true, AllowTool: []string{"destructive"}},
		{AllowWrite: true, AllowTool: []string{"all"}},
	}
	forbiddenTools := []string{"drive_upload", "drive_delete", "drive_permanent_delete", "drive_permanently_delete"}
	for _, cmd := range selectors {
		tools := mcpEnabledTools(cmd)
		for _, forbidden := range forbiddenTools {
			if hasMCPTool(tools, forbidden) {
				t.Fatalf("selector %#v exposed forbidden tool %q", cmd.AllowTool, forbidden)
			}
		}
	}
	allowedDestructive := []string{"drive_trash", "drive_share_user", "drive_unshare"}
	for _, name := range allowedDestructive {
		if !hasMCPTool(mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{"destructive"}}), name) {
			t.Fatalf("destructive selector omitted allowed Drive mutation %q", name)
		}
		if !hasMCPTool(mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{name}}), name) {
			t.Fatalf("exact destructive selector omitted allowed Drive mutation %q", name)
		}
		for _, selector := range []string{"write", "drive", "drive.*", "all", "*"} {
			if hasMCPTool(mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{selector}}), name) {
				t.Fatalf("ordinary selector %q exposed allowed destructive Drive mutation %q", selector, name)
			}
		}
	}

	for _, spec := range mcpAllTools() {
		if spec.Service != "drive" && spec.Service != "docs" && spec.Service != "sheets" && spec.Service != "slides" {
			continue
		}
		properties := newMCPTool(spec).InputSchema.Properties
		for _, field := range []string{"local_path", "path", "stdin", "argv", "output_path", "permanent", "replacements_file", "markdown_file"} {
			if _, exposed := properties[field]; exposed {
				t.Fatalf("tool %q exposes forbidden field %q", spec.Name, field)
			}
		}
	}

	calls := []struct {
		tool string
		args map[string]any
	}{
		{"drive_search", map[string]any{"query": "text"}},
		{"drive_get", map[string]any{"file_id": "f1"}},
		{"drive_list_folder", nil},
		{"drive_permissions", map[string]any{"file_id": "f1"}},
		{"drive_create_folder", map[string]any{"name": "Folder"}},
		{"drive_rename", map[string]any{"file_id": "f1", "new_name": "Name"}},
		{"drive_move", map[string]any{"file_id": "f1", "destination_parent": "p1"}},
		{"drive_copy", map[string]any{"source_id": "f1", "new_name": "Copy"}},
		{"drive_create_shortcut", map[string]any{"target_id": "f1", "parent_id": "p1"}},
		{"drive_create_comment", map[string]any{"file_id": "f1", "content": "Comment"}},
		{"docs_create", map[string]any{"title": "Doc"}},
		{"sheets_create", map[string]any{"title": "Sheet"}},
		{"slides_create_from_template", map[string]any{"template_id": "tpl", "title": "Deck", "replacements": []string{"key=value"}}},
	}
	for _, call := range calls {
		args, err := findMCPTool(t, call.tool).BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: call.args}})
		if err != nil {
			t.Fatalf("%s BuildArgs: %v", call.tool, err)
		}
		joined := strings.Join(args, "\x00")
		for _, forbidden := range []string{"drive\x00upload", "drive\x00delete", "--permanent", "@file", "local_path"} {
			if strings.Contains(joined, forbidden) {
				t.Fatalf("%s argv contains forbidden operation/input %q: %#v", call.tool, forbidden, args)
			}
		}
	}
}

func TestMCPWaveAClosedSchemasRejectGenericArgvBeforeHandlers(t *testing.T) {
	s := newMCPServer()
	handlerCalls := 0
	for _, spec := range mcpAllTools() {
		s.AddTool(newMCPTool(spec), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			handlerCalls++
			return mcp.NewToolResultText("unexpected"), nil
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
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "gog-wave-a-test", Version: "1"}
	if _, err := client.Initialize(t.Context(), initRequest); err != nil {
		t.Fatal(err)
	}

	for _, spec := range mcpAllTools() {
		result, err := client.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{
			Name:      spec.Name,
			Arguments: map[string]any{"argv": []any{"arbitrary", "command"}},
		}})
		if err != nil {
			t.Fatalf("%s: %v", spec.Name, err)
		}
		if !result.IsError || !strings.Contains(mcpResultText(result), "argv") {
			t.Errorf("%s accepted generic argv: %#v", spec.Name, result.Content)
		}
	}
	if handlerCalls != 0 {
		t.Fatalf("schema-invalid calls reached handlers %d times", handlerCalls)
	}
}

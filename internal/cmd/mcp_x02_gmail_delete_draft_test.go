package cmd

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestMCPX02GmailDeleteDraftAdapterContract(t *testing.T) {
	tool := findMCPTool(t, "gmail_delete_draft")
	if tool.Risk != mcpRiskDestructive {
		t.Fatalf("risk = %q, want %q", tool.Risk, mcpRiskDestructive)
	}
	assertMCPGmailSchema(t, "gmail_delete_draft", []string{"draft_id"}, []string{"draft_id"})
	assertMCPGmailArgv(t, "gmail_delete_draft", map[string]any{"draft_id": " --draft-1 "}, []string{
		"gmail", "drafts", "delete", "--force", "--", "--draft-1",
	})

	if _, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{"draft_id": " \t\n"},
	}}); err == nil || !strings.Contains(err.Error(), "empty draft_id") {
		t.Fatalf("empty draft_id error = %v", err)
	}

	for _, excluded := range []string{
		"force", "send", "path", "batch", "query", "max", "argv", "args", "stdin", "output",
		"local_path", "command", "command_args",
	} {
		result, calls := callMCPGmailSchema(t, "gmail_delete_draft", map[string]any{
			"draft_id": "d1",
			excluded:   true,
		})
		if !result.IsError || calls != 0 || !strings.Contains(mcpResultText(result), excluded) {
			t.Fatalf("excluded field %q result = %#v, handler calls = %d", excluded, result.Content, calls)
		}
	}
}

func TestMCPX02GmailDeleteDraftPolicyRequiresExplicitDestructiveAuthorization(t *testing.T) {
	tool := findMCPTool(t, "gmail_delete_draft")
	tests := []struct {
		name        string
		allowWrite  bool
		selectors   []string
		wantVisible bool
	}{
		{name: "default"},
		{name: "write authorization only", allowWrite: true},
		{name: "exact selector without write", selectors: []string{"gmail_delete_draft"}},
		{name: "destructive selector without write", selectors: []string{"destructive"}},
		{name: "ordinary write selector", allowWrite: true, selectors: []string{"write"}},
		{name: "service selector", allowWrite: true, selectors: []string{"gmail"}},
		{name: "service wildcard", allowWrite: true, selectors: []string{"gmail.*"}},
		{name: "all selector", allowWrite: true, selectors: []string{"all"}},
		{name: "star selector", allowWrite: true, selectors: []string{"*"}},
		{name: "destructive selector", allowWrite: true, selectors: []string{"destructive"}, wantVisible: true},
		{name: "exact selector", allowWrite: true, selectors: []string{"gmail_delete_draft"}, wantVisible: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mcpToolVisible(tool, tt.allowWrite, tt.selectors); got != tt.wantVisible {
				t.Fatalf("mcpToolVisible(%t, %#v) = %t, want %t", tt.allowWrite, tt.selectors, got, tt.wantVisible)
			}
			cmd := McpCmd{AllowWrite: tt.allowWrite, AllowTool: tt.selectors}
			if got := hasMCPTool(mcpEnabledTools(cmd), tool.Name); got != tt.wantVisible {
				t.Fatalf("mcpEnabledTools(%#v) contains tool = %t, want %t", cmd, got, tt.wantVisible)
			}
		})
	}
}

func TestMCPX02GmailDeleteDraftRunnerProviderFixture(t *testing.T) {
	t.Setenv("GOG_MCP_X02_GMAIL_DELETE_HELPER", "provider")
	tool := findMCPTool(t, "gmail_delete_draft")
	commandArgs := mcpGmailBuildArgs(t, tool.Name, map[string]any{"draft_id": "draft-child"})
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self: os.Args[0],
		tool: tool,
		baseArgs: []string{
			"-test.run=TestMCPX02GmailDeleteDraftRunnerChild$", "--",
			"--json", "--account", "mcp@example.com",
		},
		commandArgs:    commandArgs,
		timeout:        5 * time.Second,
		maxOutputBytes: 16 * 1024,
	})
	got := requireMCPGmailCommandResult(t, result)
	if result.IsError || got.ExitCode != 0 {
		t.Fatalf("provider runner result = %#v", got)
	}
	if got.Tool != tool.Name || got.Service != "gmail" || got.Risk != string(mcpRiskDestructive) {
		t.Fatalf("provider runner metadata = %#v", got)
	}
	if got.Stderr != "" {
		t.Fatalf("provider runner stderr = %q", got.Stderr)
	}
	stdout, ok := got.Stdout.(map[string]any)
	if !ok {
		t.Fatalf("provider stdout type = %T, value=%#v", got.Stdout, got.Stdout)
	}
	if stdout["deleted"] != true || stdout["draftId"] != "draft-child" {
		t.Fatalf("provider runner stdout = %#v", stdout)
	}
}

func TestMCPX02GmailDeleteDraftRunnerDryRun(t *testing.T) {
	t.Setenv("GOG_MCP_X02_GMAIL_DELETE_HELPER", "dry-run")
	tool := findMCPTool(t, "gmail_delete_draft")
	commandArgs := mcpGmailBuildArgs(t, tool.Name, map[string]any{"draft_id": "draft-dry-run"})
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self: os.Args[0],
		tool: tool,
		baseArgs: []string{
			"-test.run=TestMCPX02GmailDeleteDraftRunnerChild$", "--",
			"--json", "--dry-run", "--no-input", "--account", "mcp@example.com",
		},
		commandArgs:    commandArgs,
		timeout:        5 * time.Second,
		maxOutputBytes: 16 * 1024,
	})
	got := requireMCPGmailCommandResult(t, result)
	if result.IsError || got.ExitCode != 0 {
		t.Fatalf("dry-run runner result = %#v", got)
	}
	if got.Tool != tool.Name || got.Service != "gmail" || got.Risk != string(mcpRiskDestructive) {
		t.Fatalf("dry-run runner metadata = %#v", got)
	}
	if got.Stderr != "" {
		t.Fatalf("dry-run runner stderr = %q", got.Stderr)
	}
	stdout, ok := got.Stdout.(map[string]any)
	if !ok {
		t.Fatalf("dry-run stdout type = %T, value=%#v", got.Stdout, got.Stdout)
	}
	if stdout["dry_run"] != true || stdout["op"] != "gmail.drafts.delete" {
		t.Fatalf("dry-run stdout = %#v", stdout)
	}
	request, ok := stdout["request"].(map[string]any)
	if !ok || request["draft_id"] != "draft-dry-run" {
		t.Fatalf("dry-run request = %#v", stdout["request"])
	}
}

func TestMCPX02GmailDeleteDraftRunnerChild(t *testing.T) {
	mode := os.Getenv("GOG_MCP_X02_GMAIL_DELETE_HELPER")
	if mode == "" {
		return
	}
	cliArgs := mcpX02ChildCLIArgs()
	if len(cliArgs) == 0 {
		t.Fatalf("child did not receive CLI arguments")
	}

	var result executeTestResult
	switch mode {
	case "dry-run":
		result = executeWithTestRuntime(t, cliArgs, nil)
	case "provider":
		var deleteCalls int
		svc, closeService := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete || !strings.Contains(r.URL.Path, "draft-child") {
				http.NotFound(w, r)
				return
			}
			deleteCalls++
			w.WriteHeader(http.StatusNoContent)
		})
		defer closeService()
		result = executeWithGmailTestService(t, cliArgs, svc)
		if deleteCalls != 1 {
			result.stderr += "provider fixture delete calls = " + strconv.Itoa(deleteCalls) + "\n"
		}
	default:
		t.Fatalf("unknown child mode %q", mode)
	}
	mcpNativeEmitExecuteResult(result)
}

func mcpX02ChildCLIArgs() []string {
	args := os.Args[1:]
	for i, arg := range args {
		if arg == "--" {
			return append([]string(nil), args[i+1:]...)
		}
	}
	return nil
}

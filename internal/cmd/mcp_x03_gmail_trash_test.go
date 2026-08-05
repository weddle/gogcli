package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"google.golang.org/api/gmail/v1"
)

func TestMCPWaveDX03GmailTrashBuildArgsAndSchema(t *testing.T) {
	assertMCPGmailSchema(t, "gmail_trash_messages", []string{"message_ids"}, []string{"message_ids"})
	assertMCPGmailArgv(t, "gmail_trash_messages", map[string]any{
		"message_ids": []string{"m1", "m2"},
	}, []string{"gmail", "trash", "m1", "m2"})

	tool := findMCPTool(t, "gmail_trash_messages")
	for _, test := range []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "missing", args: map[string]any{}, want: "required argument"},
		{name: "empty", args: map[string]any{"message_ids": []string{}}, want: "at least one"},
		{name: "empty element", args: map[string]any{"message_ids": []string{"m1", " \t"}}, want: "must not be empty"},
		{name: "wrong item type", args: map[string]any{"message_ids": []any{"m1", 2}}, want: "must be a string"},
		{name: "wrong array type", args: map[string]any{"message_ids": "m1"}, want: "not an array"},
		{name: "flag injection", args: map[string]any{"message_ids": []string{"--query"}}, want: "letters and digits"},
		{name: "punctuation", args: map[string]any{"message_ids": []string{"m-1"}}, want: "letters and digits"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: test.args}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("BuildArgs error = %v, want %q", err, test.want)
			}
		})
	}

	ids := make([]string, 1000)
	for i := range ids {
		ids[i] = fmt.Sprintf("m%04d", i)
	}
	args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
		"message_ids": ids,
	}}})
	if err != nil {
		t.Fatalf("1,000 IDs: %v", err)
	}
	if len(args) != 1002 || !reflect.DeepEqual(args[:2], []string{"gmail", "trash"}) || !reflect.DeepEqual(args[2:], ids) {
		t.Fatalf("1,000-ID argv = len %d %#v, want exact gmail trash IDs", len(args), args[:6])
	}
	overflow := append(append([]string(nil), ids...), "overflow")
	if _, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
		"message_ids": overflow,
	}}}); err == nil {
		t.Fatal("expected 1,001 IDs to be rejected")
	}

	for _, excluded := range []string{"query", "max", "thread", "permanent", "delete", "force", "argv"} {
		t.Run("schema excludes "+excluded, func(t *testing.T) {
			result, calls := callMCPGmailSchema(t, "gmail_trash_messages", map[string]any{
				"message_ids": []any{"m1"},
				excluded:      "unexpected",
			})
			if !result.IsError || calls != 0 || !strings.Contains(mcpResultText(result), excluded) {
				t.Fatalf("excluded field %q reached child: result=%#v calls=%d", excluded, result.Content, calls)
			}
		})
	}
}

func TestMCPWaveDX03GmailTrashDestructivePolicy(t *testing.T) {
	spec := findMCPTool(t, "gmail_trash_messages")
	if spec.Risk != mcpRiskDestructive {
		t.Fatalf("risk = %q, want destructive", spec.Risk)
	}
	if hasMCPTool(mcpEnabledTools(McpCmd{}), spec.Name) {
		t.Fatal("destructive Gmail trash tool exposed by default")
	}
	for _, selector := range []string{"gmail", "gmail.*", "write", "*", "all", "read"} {
		if hasMCPTool(mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{selector}}), spec.Name) {
			t.Fatalf("broad selector %q exposed destructive Gmail trash tool", selector)
		}
	}
	for _, selector := range []string{"destructive", spec.Name} {
		if !hasMCPTool(mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{selector}}), spec.Name) {
			t.Fatalf("explicit destructive selector %q did not expose Gmail trash tool", selector)
		}
	}
	if hasMCPTool(mcpEnabledTools(McpCmd{AllowTool: []string{"destructive"}}), spec.Name) {
		t.Fatal("destructive selector bypassed ordinary write authorization")
	}
	if hasMCPTool(mcpEnabledToolsNoPolicy(McpCmd{AllowWrite: true, AllowTool: []string{"destructive"}}, &RootFlags{ReadOnly: true}), spec.Name) {
		t.Fatal("readonly runtime exposed destructive Gmail trash tool")
	}
}

func TestMCPWaveDX03GmailTrashProviderPreservesRecoverableTrash(t *testing.T) {
	var (
		batchCalls int
		batch      gmail.BatchModifyMessagesRequest
	)
	svc, closeService := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			t.Fatalf("trash must not invoke permanent delete: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/users/me/labels"):
			_ = json.NewEncoder(w).Encode(map[string]any{"labels": []map[string]any{
				{"id": "INBOX", "name": "INBOX"},
				{"id": "TRASH", "name": "TRASH"},
			}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/users/me/messages/batchModify"):
			batchCalls++
			if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
				t.Fatalf("decode batch modify: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			http.NotFound(w, r)
		}
	})
	defer closeService()

	result := executeWithGmailTestService(t, []string{
		"--json", "--account", "mcp@example.com", "gmail", "trash", "m1", "m2",
	}, svc)
	if result.err != nil {
		t.Fatalf("gmail trash: %v\nstderr=%s", result.err, result.stderr)
	}
	if batchCalls != 1 {
		t.Fatalf("BatchModify calls = %d, want 1", batchCalls)
	}
	if !reflect.DeepEqual(batch.Ids, []string{"m1", "m2"}) {
		t.Fatalf("BatchModify IDs = %#v, want [m1 m2]", batch.Ids)
	}
	if !reflect.DeepEqual(batch.AddLabelIds, []string{"TRASH"}) || !reflect.DeepEqual(batch.RemoveLabelIds, []string{"INBOX"}) {
		t.Fatalf("BatchModify labels = add %#v remove %#v, want TRASH/INBOX", batch.AddLabelIds, batch.RemoveLabelIds)
	}
	out := decodeMCPGmailJSON(t, result.stdout)
	if out["action"] != "trashed" || out["count"] != float64(2) {
		t.Fatalf("trash result = %#v, want recoverable trash action/count", out)
	}
	if !reflect.DeepEqual(out["addedLabels"], []any{"TRASH"}) || !reflect.DeepEqual(out["removedLabels"], []any{"INBOX"}) {
		t.Fatalf("trash result labels = add %#v remove %#v", out["addedLabels"], out["removedLabels"])
	}
}

func TestMCPWaveDX03GmailTrashServerDryRun(t *testing.T) {
	t.Setenv("GOG_MCP_WAVE_D_X03_GMAIL_TRASH_DRY_RUN_HELPER", "1")
	tool := findMCPTool(t, "gmail_trash_messages")
	commandArgs, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
		"message_ids": []string{"m1", "m2"},
	}}})
	if err != nil {
		t.Fatalf("BuildArgs: %v", err)
	}
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self: os.Args[0],
		tool: tool,
		baseArgs: []string{
			"-test.run=TestMCPWaveDX03GmailTrashServerDryRunHelper$", "--",
			"--json", "--dry-run", "--account", "mcp@example.com",
		},
		commandArgs:    commandArgs,
		timeout:        5 * time.Second,
		maxOutputBytes: 16 * 1024,
	})
	if result.IsError {
		t.Fatalf("dry-run runner result = %#v", result.StructuredContent)
	}
	got, ok := result.StructuredContent.(mcpCommandResult)
	if !ok {
		t.Fatalf("structured result type = %T, want mcpCommandResult", result.StructuredContent)
	}
	if got.Tool != tool.Name || got.Service != "gmail" || got.Risk != string(mcpRiskDestructive) || got.ExitCode != 0 {
		t.Fatalf("dry-run runner metadata = %#v", got)
	}
	stdout, ok := got.Stdout.(map[string]any)
	if !ok || stdout["op"] != "gmail.trash" {
		t.Fatalf("dry-run stdout = %#v", got.Stdout)
	}
	req, ok := stdout["request"].(map[string]any)
	if !ok {
		t.Fatalf("dry-run request = %#v", stdout["request"])
	}
	if !reflect.DeepEqual(req["message_ids"], []any{"m1", "m2"}) || req["query"] != "" || req["max"] != json.Number("100") {
		t.Fatalf("dry-run request = %#v, want explicit IDs and empty query", req)
	}
}

func TestMCPWaveDX03GmailTrashServerDryRunHelper(t *testing.T) {
	if os.Getenv("GOG_MCP_WAVE_D_X03_GMAIL_TRASH_DRY_RUN_HELPER") != "1" {
		return
	}
	args := mcpWaveDX03ChildArgs(t)
	providerCalls := 0
	svc, closeService := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		providerCalls++
		http.Error(w, "dry-run must not call provider", http.StatusInternalServerError)
	})
	result := executeWithGmailTestService(t, args, svc)
	closeService()
	if providerCalls != 0 {
		t.Fatalf("dry-run provider calls = %d, want 0", providerCalls)
	}
	_, _ = io.WriteString(os.Stdout, result.stdout)
	_, _ = io.WriteString(os.Stderr, result.stderr)
	if result.err != nil {
		os.Exit(17)
	}
	os.Exit(0)
}

func TestMCPWaveDX03GmailTrashRunnerProviderSuccess(t *testing.T) {
	t.Setenv("GOG_MCP_WAVE_D_X03_GMAIL_TRASH_RUNNER_HELPER", "success")
	tool := findMCPTool(t, "gmail_trash_messages")
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self: os.Args[0],
		tool: tool,
		baseArgs: []string{
			"-test.run=TestMCPWaveDX03GmailTrashRunnerHelper$", "--",
			"--json", "--account", "mcp@example.com",
		},
		commandArgs:    []string{"gmail", "trash", "m1", "m2"},
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
		t.Fatalf("provider runner stdout type = %T, value=%#v", got.Stdout, got.Stdout)
	}
	if stdout["action"] != "trashed" || stdout["count"] != json.Number("2") {
		t.Fatalf("provider runner aggregate = %#v", stdout)
	}
	if !reflect.DeepEqual(stdout["addedLabels"], []any{"TRASH"}) ||
		!reflect.DeepEqual(stdout["removedLabels"], []any{"INBOX"}) {
		t.Fatalf("provider runner labels = %#v", stdout)
	}
	for _, field := range []string{"failed", "results"} {
		if _, ok := stdout[field]; ok {
			t.Fatalf("provider runner aggregate unexpectedly exposed %q: %#v", field, stdout)
		}
	}
}

func TestMCPWaveDX03GmailTrashRunnerProviderFailure(t *testing.T) {
	t.Setenv("GOG_MCP_WAVE_D_X03_GMAIL_TRASH_RUNNER_HELPER", "failure")
	tool := findMCPTool(t, "gmail_trash_messages")
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self: os.Args[0],
		tool: tool,
		baseArgs: []string{
			"-test.run=TestMCPWaveDX03GmailTrashRunnerHelper$", "--",
			"--json", "--account", "mcp@example.com",
		},
		commandArgs:    []string{"gmail", "trash", "m1", "m2"},
		timeout:        5 * time.Second,
		maxOutputBytes: 16 * 1024,
	})
	got := requireMCPGmailCommandResult(t, result)
	if !result.IsError || got.ExitCode == 0 {
		t.Fatalf("provider runner failure = %#v", got)
	}
	if got.Tool != tool.Name || got.Service != "gmail" || got.Risk != string(mcpRiskDestructive) {
		t.Fatalf("provider failure metadata = %#v", got)
	}
	if got.Stdout != nil {
		t.Fatalf("provider failure stdout = %#v, want nil aggregate output", got.Stdout)
	}
	if got.Stderr == "" ||
		!strings.Contains(got.Stderr, "synthetic batch modify failure") {
		t.Fatalf("provider failure stderr = %q", got.Stderr)
	}
}

func TestMCPWaveDX03GmailTrashRunnerHelper(t *testing.T) {
	mode := os.Getenv("GOG_MCP_WAVE_D_X03_GMAIL_TRASH_RUNNER_HELPER")
	if mode == "" {
		return
	}
	args := mcpWaveDX03ChildArgs(t)
	if len(args) == 0 {
		t.Fatal("child did not receive CLI arguments")
	}

	var (
		batchCalls int
		batch      gmail.BatchModifyMessagesRequest
	)
	svc, closeService := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			t.Fatalf("trash must not invoke permanent delete: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/users/me/labels"):
			_ = json.NewEncoder(w).Encode(map[string]any{"labels": []map[string]any{
				{"id": "INBOX", "name": "INBOX"},
				{"id": "TRASH", "name": "TRASH"},
			}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/users/me/messages/batchModify"):
			batchCalls++
			if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
				t.Fatalf("decode batch modify: %v", err)
			}
			if !reflect.DeepEqual(batch.Ids, []string{"m1", "m2"}) ||
				!reflect.DeepEqual(batch.AddLabelIds, []string{"TRASH"}) ||
				!reflect.DeepEqual(batch.RemoveLabelIds, []string{"INBOX"}) {
				t.Fatalf("BatchModify aggregate request = %#v", batch)
			}
			if mode == "failure" {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{
						"code":    http.StatusInternalServerError,
						"message": "synthetic batch modify failure",
					},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			http.NotFound(w, r)
		}
	})
	result := executeWithGmailTestService(t, args, svc)
	closeService()
	if batchCalls != 1 {
		result.stderr += fmt.Sprintf("BatchModify calls = %d, want 1\n", batchCalls)
	}
	mcpNativeEmitExecuteResult(result)
}

func mcpWaveDX03ChildArgs(t *testing.T) []string {
	t.Helper()
	for i, arg := range os.Args {
		if arg == "--" {
			return append([]string(nil), os.Args[i+1:]...)
		}
	}
	t.Fatal("child argv missing -- separator")
	return nil
}

package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"google.golang.org/api/gmail/v1"
)

func mcpGmailBuildArgs(t *testing.T, toolName string, arguments map[string]any) []string {
	t.Helper()
	args, err := findMCPTool(t, toolName).BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: arguments,
	}})
	if err != nil {
		t.Fatalf("%s BuildArgs: %v", toolName, err)
	}
	return args
}

func assertMCPGmailArgv(t *testing.T, toolName string, arguments map[string]any, want []string) {
	t.Helper()
	got := mcpGmailBuildArgs(t, toolName, arguments)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s argv = %#v, want %#v", toolName, got, want)
	}
}

func assertMCPGmailSchema(t *testing.T, toolName string, required, properties []string) {
	t.Helper()
	tool := newMCPTool(findMCPTool(t, toolName))
	closed, ok := tool.InputSchema.AdditionalProperties.(bool)
	if !ok || closed {
		t.Fatalf("%s schema AdditionalProperties = %#v, want false", toolName, tool.InputSchema.AdditionalProperties)
	}
	if len(tool.InputSchema.Required) != len(required) {
		t.Fatalf("%s required = %#v, want %#v", toolName, tool.InputSchema.Required, required)
	}
	for i, name := range required {
		if tool.InputSchema.Required[i] != name {
			t.Fatalf("%s required = %#v, want %#v", toolName, tool.InputSchema.Required, required)
		}
	}
	got := make([]string, 0, len(tool.InputSchema.Properties))
	for name := range tool.InputSchema.Properties {
		got = append(got, name)
	}
	for _, name := range properties {
		found := false
		for _, gotName := range got {
			if gotName == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s missing schema property %q: %#v", toolName, name, got)
		}
	}
	if len(got) != len(properties) {
		t.Fatalf("%s properties = %#v, want exactly %#v", toolName, got, properties)
	}
}

func callMCPGmailSchema(t *testing.T, toolName string, arguments map[string]any) (*mcp.CallToolResult, int) {
	t.Helper()
	s := newMCPServer()
	handlerCalls := 0
	s.AddTool(newMCPTool(findMCPTool(t, toolName)), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handlerCalls++
		return mcp.NewToolResultText("handler reached"), nil
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
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "gog-gmail-wave-a-test", Version: "1"}
	if _, initErr := client.Initialize(t.Context(), initRequest); initErr != nil {
		t.Fatalf("initialize MCP client: %v", initErr)
	}
	result, err := client.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name:      toolName,
		Arguments: arguments,
	}})
	if err != nil {
		t.Fatalf("call %s: %v", toolName, err)
	}
	return result, handlerCalls
}

func runMCPGmailCLI(t *testing.T, toolName string, arguments map[string]any, svc *gmail.Service) executeTestResult {
	t.Helper()
	commandArgs := mcpGmailBuildArgs(t, toolName, arguments)
	args := append([]string{"--json", "--account", "mcp@example.com"}, commandArgs...)
	return executeWithGmailTestService(t, args, svc)
}

func decodeMCPGmailJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode JSON output %q: %v", raw, err)
	}
	return out
}

func mapSlice(t *testing.T, value any, field string) []map[string]any {
	t.Helper()
	raw, ok := value.([]any)
	if !ok {
		t.Fatalf("%s = %T (%#v), want array", field, value, value)
	}
	out := make([]map[string]any, 0, len(raw))
	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("%s[%d] = %T (%#v), want object", field, i, item, item)
		}
		out = append(out, m)
	}
	return out
}

func TestMCPWaveAGmailReadAdaptersHaveClosedSchemasAndExactArgv(t *testing.T) {
	assertMCPGmailSchema(t, "gmail_list_labels", nil, nil)
	assertMCPGmailArgv(t, "gmail_list_labels", nil, []string{"gmail", "labels", "list"})
	if result, calls := callMCPGmailSchema(t, "gmail_list_labels", map[string]any{"extra": true}); !result.IsError || calls != 0 || !strings.Contains(mcpResultText(result), "extra") {
		t.Fatalf("list-labels unknown field result = %#v, handler calls = %d", result.Content, calls)
	}

	assertMCPGmailSchema(t, "gmail_list_drafts", nil, []string{"max", "page_token"})
	assertMCPGmailArgv(t, "gmail_list_drafts", map[string]any{"max": 2, "page_token": "cursor"}, []string{
		"gmail", "drafts", "list", "--max", "2", "--page", "cursor",
	})
	for _, tc := range []struct {
		name string
		args map[string]any
		text string
	}{
		{name: "wrong max", args: map[string]any{"max": "2"}, text: "max"},
		{name: "zero max", args: map[string]any{"max": 0}, text: "max"},
		{name: "too large max", args: map[string]any{"max": 101}, text: "max"},
		{name: "unknown all pages", args: map[string]any{"all": true}, text: "all"},
		{name: "unknown fail empty", args: map[string]any{"fail_empty": true}, text: "fail_empty"},
	} {
		t.Run("list-drafts/"+tc.name, func(t *testing.T) {
			result, calls := callMCPGmailSchema(t, "gmail_list_drafts", tc.args)
			if !result.IsError || calls != 0 || !strings.Contains(mcpResultText(result), tc.text) {
				t.Fatalf("result = %#v, handler calls = %d, want schema rejection containing %q", result.Content, calls, tc.text)
			}
		})
	}

	assertMCPGmailSchema(t, "gmail_get_draft", []string{"draft_id"}, []string{"draft_id"})
	assertMCPGmailArgv(t, "gmail_get_draft", map[string]any{"draft_id": "--draft"}, []string{
		"gmail", "drafts", "get", "--", "--draft",
	})
	if _, err := findMCPTool(t, "gmail_get_draft").BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{"draft_id": " \t"},
	}}); err == nil || !strings.Contains(err.Error(), "empty draft_id") {
		t.Fatalf("empty draft_id error = %v", err)
	}
	for _, tc := range []struct {
		name string
		args map[string]any
		text string
	}{
		{name: "missing id", args: map[string]any{}, text: "draft_id"},
		{name: "wrong id", args: map[string]any{"draft_id": 12}, text: "draft_id"},
		{name: "download excluded", args: map[string]any{"draft_id": "d1", "download": true}, text: "download"},
		{name: "attachment id excluded", args: map[string]any{"draft_id": "d1", "attachment_id": "a1"}, text: "attachment_id"},
	} {
		t.Run("get-draft/"+tc.name, func(t *testing.T) {
			result, calls := callMCPGmailSchema(t, "gmail_get_draft", tc.args)
			if !result.IsError || calls != 0 || !strings.Contains(mcpResultText(result), tc.text) {
				t.Fatalf("result = %#v, handler calls = %d, want schema rejection containing %q", result.Content, calls, tc.text)
			}
		})
	}
}

func TestMCPWaveAGmailReadCommandsPreserveLabelAndDraftPageEnvelopes(t *testing.T) {
	var labelsCalls, draftsCalls int
	svc, closeService := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/users/me/labels"):
			labelsCalls++
			if r.URL.Query().Get("pageToken") != "" {
				t.Fatalf("labels unexpectedly received pageToken: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"labels": []map[string]any{
					{
						"id":                    "INBOX",
						"name":                  "INBOX",
						"type":                  "system",
						"labelListVisibility":   "labelShow",
						"messageListVisibility": "show",
						"messagesTotal":         42,
						"messagesUnread":        7,
						"threadsTotal":          20,
						"threadsUnread":         4,
					},
				},
			})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/users/me/drafts"):
			draftsCalls++
			if got := r.URL.Query().Get("maxResults"); got != "2" {
				t.Fatalf("drafts maxResults = %q, want 2", got)
			}
			if got := r.URL.Query().Get("pageToken"); got != "cursor" {
				t.Fatalf("drafts pageToken = %q, want cursor", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"drafts": []map[string]any{
					{"id": "d1", "message": map[string]any{"id": "m1", "threadId": "t1"}},
					{"id": "d2", "message": map[string]any{"id": "m2", "threadId": "t2"}},
				},
				"nextPageToken": "next-cursor",
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer closeService()

	labelsResult := runMCPGmailCLI(t, "gmail_list_labels", nil, svc)
	if labelsResult.err != nil {
		t.Fatalf("list labels: %v\nstderr=%s", labelsResult.err, labelsResult.stderr)
	}
	labels := decodeMCPGmailJSON(t, labelsResult.stdout)
	labelItems := mapSlice(t, labels["labels"], "labels")
	if len(labelItems) != 1 {
		t.Fatalf("labels = %#v, want one full label", labels["labels"])
	}
	label := labelItems[0]
	for key, want := range map[string]any{
		"id": "INBOX", "name": "INBOX", "type": "system", "labelListVisibility": "labelShow",
		"messageListVisibility": "show", "messagesTotal": float64(42), "messagesUnread": float64(7),
		"threadsTotal": float64(20), "threadsUnread": float64(4),
	} {
		if label[key] != want {
			t.Fatalf("label[%q] = %#v, want %#v (full label envelope lost)", key, label[key], want)
		}
	}

	draftsResult := runMCPGmailCLI(t, "gmail_list_drafts", map[string]any{"max": 2, "page_token": "cursor"}, svc)
	if draftsResult.err != nil {
		t.Fatalf("list drafts: %v\nstderr=%s", draftsResult.err, draftsResult.stderr)
	}
	drafts := decodeMCPGmailJSON(t, draftsResult.stdout)
	if drafts["nextPageToken"] != "next-cursor" {
		t.Fatalf("nextPageToken = %#v, want next-cursor", drafts["nextPageToken"])
	}
	draftItems := mapSlice(t, drafts["drafts"], "drafts")
	wantDrafts := []map[string]any{
		{"id": "d1", "messageId": "m1", "threadId": "t1"},
		{"id": "d2", "messageId": "m2", "threadId": "t2"},
	}
	if !reflect.DeepEqual(draftItems, wantDrafts) {
		t.Fatalf("draft page envelope = %#v, want %#v", draftItems, wantDrafts)
	}
	if labelsCalls != 1 || draftsCalls != 1 {
		t.Fatalf("API calls labels=%d drafts=%d, want one page call each", labelsCalls, draftsCalls)
	}
}

func TestMCPWaveAGmailGetDraftCannotDownloadOrWriteFiles(t *testing.T) {
	attachmentCalls := 0
	svc, closeService := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/users/me/drafts/d1"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "d1",
				"message": map[string]any{
					"id": "m1", "threadId": "t1",
					"payload": map[string]any{
						"mimeType": "multipart/mixed",
						"parts": []map[string]any{{
							"filename": "secret.txt", "mimeType": "text/plain",
							"body": map[string]any{"attachmentId": "att-1", "size": 12},
						}},
					},
				},
			})
		case strings.Contains(r.URL.Path, "/attachments/"):
			attachmentCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{"data": encodeBase64URL("secret")})
		default:
			http.NotFound(w, r)
		}
	})
	defer closeService()

	before := t.TempDir()
	t.Setenv("HOME", before)
	t.Setenv("XDG_CONFIG_HOME", before)
	result := runMCPGmailCLI(t, "gmail_get_draft", map[string]any{"draft_id": "d1"}, svc)
	if result.err != nil {
		t.Fatalf("get draft: %v\nstderr=%s", result.err, result.stderr)
	}
	out := decodeMCPGmailJSON(t, result.stdout)
	if out["draft"] == nil {
		t.Fatalf("MCP get-draft lost draft envelope: %#v", out)
	}
	if _, ok := out["downloaded"]; ok {
		t.Fatalf("MCP get-draft unexpectedly emitted downloaded output: %#v", out)
	}
	if attachmentCalls != 0 {
		t.Fatalf("attachment API calls = %d, want 0", attachmentCalls)
	}
	entries, err := os.ReadDir(before)
	if err != nil {
		t.Fatalf("read temporary config dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("get-draft wrote files despite no download input: %#v", entries)
	}
}

func TestMCPWaveAGmailGetThreadBuildArgsAndClosedSchema(t *testing.T) {
	t.Run("read-only default exposure", func(t *testing.T) {
		tool := findMCPTool(t, "gmail_get_thread")
		if tool.Risk != mcpRiskRead {
			t.Fatalf("risk = %q, want read", tool.Risk)
		}
		if !hasMCPTool(mcpEnabledTools(McpCmd{}), tool.Name) {
			t.Fatal("gmail_get_thread missing from default read-only policy")
		}
	})

	assertMCPGmailSchema(t, "gmail_get_thread", []string{"thread_id"}, []string{
		"thread_id", "sanitize_content", "full",
	})
	for _, tc := range []struct {
		name      string
		arguments map[string]any
		want      []string
	}{
		{
			name:      "default sanitization and separator",
			arguments: map[string]any{"thread_id": "thread-1"},
			want:      []string{"gmail", "thread", "get", "--sanitize-content", "--", "thread-1"},
		},
		{
			name:      "explicit sanitization preserves leading-dash ID",
			arguments: map[string]any{"thread_id": "--thread-1", "sanitize_content": true},
			want:      []string{"gmail", "thread", "get", "--sanitize-content", "--", "--thread-1"},
		},
		{
			name:      "explicit raw content",
			arguments: map[string]any{"thread_id": "thread-1", "sanitize_content": false},
			want:      []string{"gmail", "thread", "get", "--", "thread-1"},
		},
		{
			name:      "full sanitized content",
			arguments: map[string]any{"thread_id": "thread-1", "full": true},
			want:      []string{"gmail", "thread", "get", "--sanitize-content", "--full", "--", "thread-1"},
		},
		{
			name:      "full raw content",
			arguments: map[string]any{"thread_id": "thread-1", "sanitize_content": false, "full": true},
			want:      []string{"gmail", "thread", "get", "--full", "--", "thread-1"},
		},
	} {
		t.Run("argv/"+tc.name, func(t *testing.T) {
			assertMCPGmailArgv(t, "gmail_get_thread", tc.arguments, tc.want)
		})
	}

	invalidSchema := []struct {
		name string
		args map[string]any
		text string
	}{
		{name: "missing thread ID", args: map[string]any{}, text: "thread_id"},
		{name: "wrong thread ID type", args: map[string]any{"thread_id": 42}, text: "thread_id"},
		{name: "wrong sanitize type", args: map[string]any{"thread_id": "thread-1", "sanitize_content": "true"}, text: "sanitize_content"},
		{name: "wrong full type", args: map[string]any{"thread_id": "thread-1", "full": "true"}, text: "full"},
		{name: "unknown field", args: map[string]any{"thread_id": "thread-1", "unknown": true}, text: "unknown"},
	}
	for _, tc := range invalidSchema {
		t.Run("schema/"+tc.name, func(t *testing.T) {
			result, calls := callMCPGmailSchema(t, "gmail_get_thread", tc.args)
			if !result.IsError || calls != 0 || !strings.Contains(mcpResultText(result), tc.text) {
				t.Fatalf("result = %#v, handler calls = %d, want schema rejection containing %q", result.Content, calls, tc.text)
			}
		})
	}

	emptyResult, emptyChildCalls := callMCPGmailThreadBuildArgs(t, map[string]any{"thread_id": " \t\n"})
	if !emptyResult.IsError || emptyChildCalls != 0 || !strings.Contains(mcpResultText(emptyResult), "empty thread_id") {
		t.Fatalf("empty thread_id result = %#v, child calls = %d", emptyResult.Content, emptyChildCalls)
	}

	for _, excluded := range []string{
		"download", "path", "out_dir", "output_dir", "local_path",
		"stdin", "at_file", "args", "argv", "query", "max", "all",
		"page_token", "format", "attachment_id", "body", "body_file",
		"account", "home", "client",
	} {
		t.Run("excluded/"+excluded, func(t *testing.T) {
			result, calls := callMCPGmailSchema(t, "gmail_get_thread", map[string]any{
				"thread_id": "thread-1",
				excluded:    "rejected",
			})
			if !result.IsError || calls != 0 || !strings.Contains(mcpResultText(result), excluded) {
				t.Fatalf("excluded %q result = %#v, handler calls = %d", excluded, result.Content, calls)
			}
		})
	}
}

func TestMCPWaveAGmailGetThreadStructuredResultThroughRunner(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode string
	}{
		{name: "default sanitized", mode: "default"},
		{name: "explicit full remains sanitized", mode: "full"},
		{name: "explicit raw and full", mode: "raw"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, got := runMCPGmailThreadChild(t, tc.mode, 16*1024)
			assertMCPGmailThreadResultMetadata(t, result, got)
			if result.IsError || got.ExitCode != 0 {
				t.Fatalf("thread %s result = %#v", tc.mode, got)
			}
			if got.Stderr != "" {
				t.Fatalf("thread %s stderr = %q, want empty on success", tc.mode, got.Stderr)
			}
			stdout, ok := got.Stdout.(map[string]any)
			if !ok {
				t.Fatalf("thread %s stdout type = %T, value=%#v", tc.mode, got.Stdout, got.Stdout)
			}
			thread, ok := stdout["thread"].(map[string]any)
			if !ok {
				t.Fatalf("thread %s envelope = %#v", tc.mode, stdout)
			}
			if thread["id"] != "thread-mcp" {
				t.Fatalf("thread %s id = %#v, want thread-mcp", tc.mode, thread["id"])
			}
			messages := mapSlice(t, thread["messages"], "thread.messages")
			if len(messages) != 1 {
				t.Fatalf("thread %s messages = %#v, want one message", tc.mode, thread["messages"])
			}
			if _, ok := stdout["downloaded"]; !ok {
				t.Fatalf("thread %s envelope omitted downloaded field: %#v", tc.mode, stdout)
			}
			message := messages[0]
			switch tc.mode {
			case "default", "full":
				if _, ok := message["payload"]; ok {
					t.Fatalf("thread %s leaked raw payload: %#v", tc.mode, message)
				}
				if message["body"] != "Hello [url removed]" {
					t.Fatalf("thread %s sanitized body = %#v", tc.mode, message["body"])
				}
				if message["snippet"] != "Snippet [url removed]" {
					t.Fatalf("thread %s sanitized snippet = %#v", tc.mode, message["snippet"])
				}
				headers, ok := message["headers"].(map[string]any)
				if !ok || headers["subject"] != "Subject [url removed]" {
					t.Fatalf("thread %s sanitized headers = %#v", tc.mode, message["headers"])
				}
			case "raw":
				payload, ok := message["payload"].(map[string]any)
				if !ok {
					t.Fatalf("raw thread payload = %T, value=%#v", message["payload"], message["payload"])
				}
				body, ok := payload["body"].(map[string]any)
				if !ok || body["data"] != mcpGmailThreadHTMLData() {
					t.Fatalf("raw thread body = %#v, want encoded source body", payload["body"])
				}
				if message["snippet"] != "Snippet https://snippet.example/path" {
					t.Fatalf("raw thread snippet = %#v", message["snippet"])
				}
			}
		})
	}
}

func TestMCPWaveAGmailGetThreadRunnerBoundsOutputAndSeparatesErrors(t *testing.T) {
	const maxOutputBytes = 256
	result, got := runMCPGmailThreadChild(t, "large", maxOutputBytes)
	assertMCPGmailThreadResultMetadata(t, result, got)
	if result.IsError || got.ExitCode != 0 {
		t.Fatalf("bounded thread result = %#v", got)
	}
	stdout, ok := got.Stdout.(string)
	if !ok {
		t.Fatalf("bounded thread stdout type = %T, value=%#v", got.Stdout, got.Stdout)
	}
	const truncationSuffix = "\n... [output truncated]"
	if !strings.HasSuffix(stdout, truncationSuffix) {
		t.Fatalf("bounded thread stdout = %q, want truncation marker", stdout)
	}
	if len(stdout) > maxOutputBytes+len(truncationSuffix) {
		t.Fatalf("bounded thread stdout length = %d, exceeds cap %d plus marker", len(stdout), maxOutputBytes)
	}
	if got.Stderr != "" {
		t.Fatalf("bounded thread stderr = %q, want separate empty stderr", got.Stderr)
	}

	result, got = runMCPGmailThreadChild(t, "error", 4096)
	assertMCPGmailThreadResultMetadata(t, result, got)
	if !result.IsError || got.ExitCode == 0 {
		t.Fatalf("thread API failure = result %#v, want MCP error with nonzero exit", got)
	}
	if got.Stdout != nil {
		t.Fatalf("thread API failure stdout = %#v, want nil", got.Stdout)
	}
	if got.Stderr == "" || !strings.Contains(got.Stderr, "synthetic thread failure") {
		t.Fatalf("thread API failure stderr = %q, want provider diagnostic only on stderr", got.Stderr)
	}
}

func callMCPGmailThreadBuildArgs(t *testing.T, arguments map[string]any) (*mcp.CallToolResult, int) {
	t.Helper()
	spec := findMCPTool(t, "gmail_get_thread")
	s := newMCPServer()
	childCalls := 0
	s.AddTool(newMCPTool(spec), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		_, err := spec.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: arguments}})
		if err != nil {
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
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "gog-gmail-thread-test", Version: "1"}
	if _, initErr := client.Initialize(t.Context(), initRequest); initErr != nil {
		t.Fatalf("initialize MCP client: %v", initErr)
	}
	result, err := client.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name:      "gmail_get_thread",
		Arguments: arguments,
	}})
	if err != nil {
		t.Fatalf("call gmail_get_thread: %v", err)
	}
	return result, childCalls
}

func runMCPGmailThreadChild(t *testing.T, mode string, maxOutputBytes int) (*mcp.CallToolResult, mcpCommandResult) {
	t.Helper()
	t.Setenv("GOG_MCP_WAVE_A_GMAIL_THREAD_HELPER", "1")
	t.Setenv("GOG_MCP_WAVE_A_GMAIL_THREAD_MODE", mode)
	arguments := map[string]any{"thread_id": "thread-mcp"}
	switch mode {
	case "full":
		arguments["full"] = true
	case "raw":
		arguments["sanitize_content"] = false
		arguments["full"] = true
	}
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self:           os.Args[0],
		tool:           findMCPTool(t, "gmail_get_thread"),
		commandArgs:    []string{"-test.run=TestMCPWaveAGmailGetThreadMCPChild$"},
		timeout:        5 * time.Second,
		maxOutputBytes: maxOutputBytes,
	})
	got := requireMCPGmailCommandResult(t, result)
	return result, got
}

func requireMCPGmailCommandResult(t *testing.T, result *mcp.CallToolResult) mcpCommandResult {
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

func assertMCPGmailThreadResultMetadata(t *testing.T, result *mcp.CallToolResult, got mcpCommandResult) {
	t.Helper()
	if result == nil {
		t.Fatal("nil MCP result")
	}
	if got.Tool != "gmail_get_thread" || got.Service != "gmail" || got.Risk != string(mcpRiskRead) {
		t.Fatalf("thread result metadata = %#v", got)
	}
}

func mcpGmailThreadHTMLData() string {
	return encodeBase64URL(`<style>.x{background:url(https://tracker.example)}</style><p>Hello https://phish.example/login</p>`)
}

func TestMCPWaveAGmailGetThreadMCPChild(t *testing.T) {
	if os.Getenv("GOG_MCP_WAVE_A_GMAIL_THREAD_HELPER") != "1" {
		return
	}
	mode := os.Getenv("GOG_MCP_WAVE_A_GMAIL_THREAD_MODE")
	const threadID = "thread-mcp"
	svc, closeService := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/users/me/threads/"+threadID) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if mode == "error" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"code":500,"message":"synthetic thread failure"}}`)
			return
		}
		body := `<style>.x{background:url(https://tracker.example)}</style><p>Hello https://phish.example/login</p>`
		if mode == "large" {
			body = strings.Repeat("safe body ", 5000)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": threadID,
			"messages": []map[string]any{{
				"id":           "message-mcp",
				"threadId":     threadID,
				"labelIds":     []string{"INBOX"},
				"snippet":      "Snippet https://snippet.example/path",
				"internalDate": "1700000000000",
				"sizeEstimate": 123,
				"payload": map[string]any{
					"mimeType": "text/html",
					"headers": []map[string]any{
						{"name": "From", "value": "Alice <alice@example.com>"},
						{"name": "Subject", "value": "Subject https://subject.example/path"},
						{"name": "Date", "value": "Mon, 02 Jan 2006 15:04:05 -0700"},
					},
					"body": map[string]any{
						"size": len(body),
						"data": encodeBase64URL(body),
					},
				},
			}},
		})
	})
	defer closeService()

	arguments := map[string]any{"thread_id": threadID}
	switch mode {
	case "full":
		arguments["full"] = true
	case "raw":
		arguments["sanitize_content"] = false
		arguments["full"] = true
	}
	args := mcpGmailBuildArgs(t, "gmail_get_thread", arguments)
	result := executeWithGmailTestService(t, append([]string{"--json", "--account", "mcp@example.com"}, args...), svc)
	mcpNativeEmitExecuteResult(result)
}

func TestMCPWaveAGmailCreateDraftSchemaFieldsAndReplyCombinations(t *testing.T) {
	assertMCPGmailSchema(t, "gmail_create_draft", nil, []string{
		"to", "cc", "bcc", "subject", "body", "body_html", "reply_to_message_id", "thread_id",
		"reply_all", "reply_to", "quote", "from",
	})
	for _, excluded := range []string{
		"body_file", "body_html_file", "attach", "attachments", "send", "auto_from_addressed_alias",
		"path", "local_path", "stdin", "argv", "args",
	} {
		result, calls := callMCPGmailSchema(t, "gmail_create_draft", map[string]any{
			"subject": "S", "body": "B", excluded: true,
		})
		if !result.IsError || calls != 0 || !strings.Contains(mcpResultText(result), excluded) {
			t.Fatalf("excluded field %q result = %#v, handler calls = %d", excluded, result.Content, calls)
		}
	}

	body := "  plain body\n\n"
	html := " <p>html body</p>\n"
	assertMCPGmailArgv(t, "gmail_create_draft", map[string]any{
		"to":                  " to@example.com ",
		"cc":                  "cc@example.com",
		"bcc":                 "bcc@example.com",
		"subject":             " Subject ",
		"body":                body,
		"body_html":           html,
		"reply_to_message_id": "m1",
		"reply_all":           true,
		"reply_to":            "reply@example.com",
		"quote":               true,
		"from":                "alias@example.com",
	}, []string{
		"gmail", "drafts", "create", "--to", "to@example.com", "--cc", "cc@example.com", "--bcc", "bcc@example.com",
		"--subject", "Subject", "--reply-to-message-id", "m1", "--reply-to", "reply@example.com", "--from", "alias@example.com",
		"--body", body, "--body-html", html, "--reply-all", "--quote",
	})
	assertMCPGmailArgv(t, "gmail_create_draft", map[string]any{
		"thread_id": "thread-1", "body": "reply body", "reply_all": true, "quote": true,
	}, []string{
		"gmail", "drafts", "create", "--thread-id", "thread-1", "--body", "reply body", "--reply-all", "--quote",
	})

	tool := findMCPTool(t, "gmail_create_draft")
	invalid := []struct {
		name string
		args map[string]any
		text string
	}{
		{name: "missing subject", args: map[string]any{"body": "B"}, text: "subject required"},
		{name: "missing body", args: map[string]any{"subject": "S"}, text: "body or body_html is required"},
		{name: "whitespace body", args: map[string]any{"subject": "S", "body": " \t\n"}, text: "body or body_html is required"},
		{name: "both reply targets", args: map[string]any{"subject": "S", "body": "B", "reply_to_message_id": "m1", "thread_id": "t1"}, text: "mutually exclusive"},
		{name: "reply all without target", args: map[string]any{"subject": "S", "body": "B", "reply_all": true}, text: "reply_all"},
		{name: "quote without target", args: map[string]any{"subject": "S", "body": "B", "quote": true}, text: "quote"},
	}
	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			_, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tc.args}})
			if err == nil || !strings.Contains(err.Error(), tc.text) {
				t.Fatalf("BuildArgs error = %v, want text %q", err, tc.text)
			}
		})
	}
	for _, tc := range []struct {
		name string
		args map[string]any
		text string
	}{
		{name: "wrong body", args: map[string]any{"subject": "S", "body": 12}, text: "body"},
		{name: "wrong reply all", args: map[string]any{"subject": "S", "body": "B", "reply_all": "true"}, text: "reply_all"},
		{name: "unknown field", args: map[string]any{"subject": "S", "body": "B", "unknown": true}, text: "unknown"},
	} {
		t.Run("schema/"+tc.name, func(t *testing.T) {
			result, calls := callMCPGmailSchema(t, "gmail_create_draft", tc.args)
			if !result.IsError || calls != 0 || !strings.Contains(mcpResultText(result), tc.text) {
				t.Fatalf("result = %#v, handler calls = %d, want schema rejection containing %q", result.Content, calls, tc.text)
			}
		})
	}
}

func TestMCPWaveAGmailCreateDraftInlineBodiesReachCLIUnchanged(t *testing.T) {
	body := "  keep leading space\nline two\n\n"
	html := " <p>keep HTML</p>\n"
	arguments := map[string]any{
		"to": "recipient@example.com", "subject": "Inline", "body": body, "body_html": html,
	}
	var rawCreated string
	svc, closeService := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/users/me/drafts") {
			http.NotFound(w, r)
			return
		}
		var draft gmail.Draft
		if err := json.NewDecoder(r.Body).Decode(&draft); err != nil {
			t.Fatalf("decode draft create: %v", err)
		}
		if draft.Message == nil {
			t.Fatalf("draft create omitted message")
		}
		decoded, err := base64.RawURLEncoding.DecodeString(draft.Message.Raw)
		if err != nil {
			t.Fatalf("decode MIME: %v", err)
		}
		rawCreated = string(decoded)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "d-inline", "message": map[string]any{"id": "m-inline"}})
	})
	defer closeService()

	result := runMCPGmailCLI(t, "gmail_create_draft", arguments, svc)
	if result.err != nil {
		t.Fatalf("create draft: %v\nstderr=%s", result.err, result.stderr)
	}
	wantBody := strings.ReplaceAll(body, "\n", "\r\n")
	wantHTML := strings.ReplaceAll(html, "\n", "\r\n")
	if !strings.Contains(rawCreated, wantBody) {
		t.Fatalf("plain body whitespace changed in MIME: %q", rawCreated)
	}
	if !strings.Contains(rawCreated, wantHTML) {
		t.Fatalf("HTML body changed in MIME: %q", rawCreated)
	}
	out := decodeMCPGmailJSON(t, result.stdout)
	if out["draftId"] != "d-inline" || out["message"] == nil {
		t.Fatalf("structured create result = %#v", out)
	}
}

func TestMCPWaveAGmailLabelMutationContractsAndResolvedOutput(t *testing.T) {
	cases := []struct {
		tool   string
		field  string
		prefix []string
		id     string
	}{
		{tool: "gmail_modify_message_labels", field: "message_id", prefix: []string{"gmail", "messages", "modify"}, id: "m1"},
		{tool: "gmail_modify_thread_labels", field: "thread_id", prefix: []string{"gmail", "thread", "modify"}, id: "t1"},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			assertMCPGmailSchema(t, tc.tool, []string{tc.field}, []string{tc.field, "add", "remove"})
			assertMCPGmailArgv(t, tc.tool, map[string]any{tc.field: tc.id, "add": "CUSTOM", "remove": "INBOX"}, append(append([]string{}, tc.prefix...), "--add", "CUSTOM", "--remove", "INBOX", "--", tc.id))
			assertMCPGmailArgv(t, tc.tool, map[string]any{tc.field: tc.id, "remove": "INBOX"}, append(append([]string{}, tc.prefix...), "--remove", "INBOX", "--", tc.id))
			for _, args := range []map[string]any{
				{tc.field: tc.id},
				{tc.field: " \t", "add": "INBOX"},
			} {
				if _, err := findMCPTool(t, tc.tool).BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}}); err == nil {
					t.Fatalf("BuildArgs accepted invalid labels input %#v", args)
				}
			}
			for _, excluded := range []string{"query", "max", "message_ids", "thread_ids", "all"} {
				result, calls := callMCPGmailSchema(t, tc.tool, map[string]any{tc.field: tc.id, "add": "INBOX", excluded: "x"})
				if !result.IsError || calls != 0 || !strings.Contains(mcpResultText(result), excluded) {
					t.Fatalf("excluded %q result = %#v, handler calls = %d", excluded, result.Content, calls)
				}
			}
		})
	}

	var modifiedMessage, modifiedThread map[string]any
	svc, closeService := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/users/me/labels"):
			_ = json.NewEncoder(w).Encode(map[string]any{"labels": []map[string]any{
				{"id": "INBOX", "name": "INBOX"},
				{"id": "Label_custom", "name": "Custom"},
			}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/users/me/messages/m1/modify"):
			if err := json.NewDecoder(r.Body).Decode(&modifiedMessage); err != nil {
				t.Fatalf("decode message modify: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "m1"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/users/me/threads/t1/modify"):
			if err := json.NewDecoder(r.Body).Decode(&modifiedThread); err != nil {
				t.Fatalf("decode thread modify: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "t1"})
		default:
			http.NotFound(w, r)
		}
	})
	defer closeService()

	for _, tc := range cases {
		result := runMCPGmailCLI(t, tc.tool, map[string]any{tc.field: tc.id, "add": "Custom", "remove": "INBOX"}, svc)
		if result.err != nil {
			t.Fatalf("%s command: %v\nstderr=%s", tc.tool, result.err, result.stderr)
		}
		out := decodeMCPGmailJSON(t, result.stdout)
		if out["modified"] != tc.id {
			t.Fatalf("%s modified = %#v, want %q", tc.tool, out["modified"], tc.id)
		}
		if !reflect.DeepEqual(out["addedLabels"], []any{"Label_custom"}) || !reflect.DeepEqual(out["removedLabels"], []any{"INBOX"}) {
			t.Fatalf("%s resolved labels = %#v, want Custom=Label_custom and INBOX", tc.tool, out)
		}
	}
	if modifiedMessage == nil || modifiedThread == nil {
		t.Fatalf("message/thread modify API requests not both observed: message=%#v thread=%#v", modifiedMessage, modifiedThread)
	}
	if !reflect.DeepEqual(modifiedMessage["addLabelIds"], []any{"Label_custom"}) || !reflect.DeepEqual(modifiedMessage["removeLabelIds"], []any{"INBOX"}) {
		t.Fatalf("message modify request = %#v", modifiedMessage)
	}
	if !reflect.DeepEqual(modifiedThread["addLabelIds"], []any{"Label_custom"}) || !reflect.DeepEqual(modifiedThread["removeLabelIds"], []any{"INBOX"}) {
		t.Fatalf("thread modify request = %#v", modifiedThread)
	}
}

func TestMCPWaveAGmailExplicitIDMutationSchemasAndBoundaries(t *testing.T) {
	cases := []struct {
		tool   string
		field  string
		prefix []string
	}{
		{tool: "gmail_archive_messages", field: "message_ids", prefix: []string{"gmail", "archive"}},
		{tool: "gmail_archive_threads", field: "thread_ids", prefix: []string{"gmail", "archive", "--thread"}},
		{tool: "gmail_mark_messages_read", field: "message_ids", prefix: []string{"gmail", "mark-read", "--"}},
		{tool: "gmail_mark_messages_unread", field: "message_ids", prefix: []string{"gmail", "unread", "--"}},
	}
	ids := make([]string, 1000)
	for i := range ids {
		ids[i] = fmt.Sprintf("id-%04d", i)
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			assertMCPGmailSchema(t, tc.tool, []string{tc.field}, []string{tc.field})
			got := mcpGmailBuildArgs(t, tc.tool, map[string]any{tc.field: ids})
			want := append(append([]string{}, tc.prefix...), ids...)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("1,000 IDs argv length/content changed: got len=%d want len=%d; first/last=%#v/%#v", len(got), len(want), got[:mcpGmailMinInt(len(got), 4)], got[mcpGmailMaxInt(0, len(got)-2):])
			}
			if !reflect.DeepEqual(got[len(tc.prefix):], ids) {
				t.Fatalf("1,000 IDs were not forwarded exactly")
			}
			for _, bad := range []map[string]any{
				{},
				{tc.field: []string{}},
				{tc.field: []string{"id-1", " \t"}},
				{tc.field: []any{"id-1", 2}},
				{tc.field: "id-1"},
			} {
				if _, err := findMCPTool(t, tc.tool).BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: bad}}); err == nil {
					t.Fatalf("BuildArgs accepted invalid explicit IDs %#v", bad)
				}
			}
			overflow := append(append([]string{}, ids...), "overflow")
			if _, err := findMCPTool(t, tc.tool).BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{tc.field: overflow}}}); err == nil {
				t.Fatal("BuildArgs accepted 1,001 IDs")
			}
			result, calls := callMCPGmailSchema(t, tc.tool, map[string]any{tc.field: overflow})
			if !result.IsError || calls != 0 {
				t.Fatalf("1,001 IDs reached child handler: result=%#v calls=%d", result.Content, calls)
			}
			for _, excluded := range []string{"query", "max", "page_token", "all_pages"} {
				result, calls := callMCPGmailSchema(t, tc.tool, map[string]any{tc.field: []any{"id-1"}, excluded: "x"})
				if !result.IsError || calls != 0 || !strings.Contains(mcpResultText(result), excluded) {
					t.Fatalf("excluded selector %q result=%#v calls=%d", excluded, result.Content, calls)
				}
			}
		})
	}
}

func mcpGmailMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func mcpGmailMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func TestMCPWaveAGmailMarkReadUnreadForwardExactly1000IDsInOneBatchModify(t *testing.T) {
	ids := make([]string, 1000)
	for i := range ids {
		ids[i] = fmt.Sprintf("message-%04d", i)
	}
	for _, tc := range []struct {
		tool       string
		action     string
		wantAdd    []string
		wantRemove []string
	}{
		{tool: "gmail_mark_messages_read", action: "marked as read", wantRemove: []string{"UNREAD"}},
		{tool: "gmail_mark_messages_unread", action: "marked as unread", wantAdd: []string{"UNREAD"}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			var batchCalls int
			var gotBatch gmail.BatchModifyMessagesRequest
			svc, closeService := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/users/me/labels"):
					_ = json.NewEncoder(w).Encode(map[string]any{"labels": []map[string]any{{"id": "UNREAD", "name": "UNREAD"}}})
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/users/me/messages/batchModify"):
					batchCalls++
					if err := json.NewDecoder(r.Body).Decode(&gotBatch); err != nil {
						t.Fatalf("decode batch modify: %v", err)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{})
				default:
					http.NotFound(w, r)
				}
			})
			defer closeService()

			args := mcpGmailBuildArgs(t, tc.tool, map[string]any{"message_ids": ids})
			wantArgs := append([]string{"gmail"}, map[string]string{
				"gmail_mark_messages_read":   "mark-read",
				"gmail_mark_messages_unread": "unread",
			}[tc.tool], "--")
			wantArgs = append(wantArgs, ids...)
			if !reflect.DeepEqual(args, wantArgs) {
				t.Fatalf("exact 1,000-ID adapter argv = len %d, want len %d", len(args), len(wantArgs))
			}
			result := executeWithGmailTestService(t, append([]string{"--json", "--account", "mcp@example.com"}, args...), svc)
			if result.err != nil {
				t.Fatalf("%s: %v\nstderr=%s", tc.tool, result.err, result.stderr)
			}
			if batchCalls != 1 {
				t.Fatalf("BatchModify calls = %d, want exactly one for 1,000 IDs", batchCalls)
			}
			if !reflect.DeepEqual(gotBatch.Ids, ids) {
				t.Fatalf("BatchModify IDs were truncated/reordered: got %d IDs, want %d", len(gotBatch.Ids), len(ids))
			}
			if !reflect.DeepEqual(gotBatch.AddLabelIds, tc.wantAdd) || !reflect.DeepEqual(gotBatch.RemoveLabelIds, tc.wantRemove) {
				t.Fatalf("BatchModify labels = add %#v remove %#v, want add %#v remove %#v", gotBatch.AddLabelIds, gotBatch.RemoveLabelIds, tc.wantAdd, tc.wantRemove)
			}
			out := decodeMCPGmailJSON(t, result.stdout)
			if out["action"] != tc.action || out["count"] != float64(1000) {
				t.Fatalf("structured mark result = %#v, want action/count for all IDs", out)
			}
		})
	}
}

func TestMCPWaveAGmailArchiveThreadPartialFailureUsesRealCLIResult(t *testing.T) {
	threadIDs := []string{"thread-ok-1", "thread-fail", "thread-ok-2"}
	var modified []string
	svc, closeService := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/modify") {
			http.NotFound(w, r)
			return
		}
		threadID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/threads/"), "/modify")
		modified = append(modified, threadID)
		if threadID == "thread-fail" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"code":500,"message":"synthetic thread failure"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": threadID})
	})
	defer closeService()

	args := mcpGmailBuildArgs(t, "gmail_archive_threads", map[string]any{"thread_ids": threadIDs})
	result := executeWithGmailTestService(t, append([]string{"--json", "--account", "mcp@example.com"}, args...), svc)
	if result.err == nil || !strings.Contains(result.err.Error(), "archived 2 of 3 threads; 1 failed") {
		t.Fatalf("archive partial error = %v, want task-defined summary", result.err)
	}
	if !reflect.DeepEqual(modified, threadIDs) {
		t.Fatalf("archive calls = %#v, want every explicit thread ID", modified)
	}
	out := decodeMCPGmailJSON(t, result.stdout)
	if out["action"] != "archived" || out["resource"] != "thread" || out["count"] != float64(2) || out["failed"] != float64(1) {
		t.Fatalf("archive partial envelope = %#v", out)
	}
	results := mapSlice(t, out["results"], "results")
	if len(results) != 3 {
		t.Fatalf("archive results = %#v, want one result per thread", results)
	}
	for i, item := range results {
		if item["threadId"] != threadIDs[i] {
			t.Fatalf("results[%d].threadId = %#v, want %q", i, item["threadId"], threadIDs[i])
		}
		success, _ := item["success"].(bool)
		if i == 1 {
			if success || item["error"] == nil {
				t.Fatalf("failed result = %#v, want error without success", item)
			}
		} else if !success || item["error"] != nil {
			t.Fatalf("successful result = %#v, want success without error", item)
		}
	}
}

func TestMCPWaveAGmailWritePolicySelectors(t *testing.T) {
	writeTools := []string{
		"gmail_create_draft", "gmail_modify_message_labels", "gmail_modify_thread_labels",
		"gmail_archive_messages", "gmail_archive_threads", "gmail_mark_messages_read", "gmail_mark_messages_unread",
	}
	for _, name := range writeTools {
		t.Run(name, func(t *testing.T) {
			spec := findMCPTool(t, name)
			if spec.Risk != mcpRiskWrite {
				t.Fatalf("risk = %q, want write", spec.Risk)
			}
			if hasMCPTool(mcpEnabledTools(McpCmd{}), name) {
				t.Fatal("ordinary Gmail write exposed by default read-only policy")
			}
			for _, selector := range []string{name, "gmail", "gmail.*", "write", "all"} {
				if !hasMCPTool(mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{selector}}), name) {
					t.Fatalf("ordinary Gmail write %q not exposed by authorized selector %q", name, selector)
				}
			}
			if hasMCPTool(mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{"read"}}), name) {
				t.Fatal("read selector widened into Gmail write")
			}
		})
	}
	for _, name := range []string{"gmail_list_labels", "gmail_list_drafts", "gmail_get_draft"} {
		if !hasMCPTool(mcpEnabledTools(McpCmd{}), name) {
			t.Fatalf("read tool %q missing from default policy", name)
		}
	}
}

func TestMCPWaveAGmailArchivePartialFailureMCPChild(t *testing.T) {
	if os.Getenv("GOG_MCP_WAVE_A_GMAIL_ARCHIVE_HELPER") != "1" {
		return
	}
	svc, _ := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/modify") {
			http.NotFound(w, r)
			return
		}
		threadID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/threads/"), "/modify")
		w.Header().Set("Content-Type", "application/json")
		if threadID == "thread-fail" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"code":500,"message":"synthetic thread failure"}}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": threadID})
	})
	result := executeWithGmailTestService(t, []string{
		"--json", "--account", "mcp@example.com", "gmail", "archive", "--thread",
		"thread-ok", "thread-fail",
	}, svc)
	_, _ = io.WriteString(os.Stdout, result.stdout)
	_, _ = io.WriteString(os.Stderr, result.stderr)
	if result.err == nil {
		os.Exit(0)
	}
	os.Exit(17)
}

func TestMCPWaveAGmailArchivePartialFailureMCPStructuredResult(t *testing.T) {
	t.Setenv("GOG_MCP_WAVE_A_GMAIL_ARCHIVE_HELPER", "1")
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self:           os.Args[0],
		tool:           findMCPTool(t, "gmail_archive_threads"),
		commandArgs:    []string{"-test.run=TestMCPWaveAGmailArchivePartialFailureMCPChild$"},
		timeout:        5 * time.Second,
		maxOutputBytes: 16 * 1024,
	})
	if !result.IsError {
		t.Fatalf("expected partial archive child failure result, got %#v", result.Content)
	}
	got, ok := result.StructuredContent.(mcpCommandResult)
	if !ok {
		t.Fatalf("structured result type = %T, want mcpCommandResult", result.StructuredContent)
	}
	if got.ExitCode != 17 {
		t.Fatalf("archive child exit code = %d, want 17; stderr=%q", got.ExitCode, got.Stderr)
	}
	stdout, ok := got.Stdout.(map[string]any)
	if !ok {
		t.Fatalf("archive child stdout type = %T, want JSON object", got.Stdout)
	}
	if stdout["action"] != "archived" || stdout["resource"] != "thread" ||
		stdout["count"] != json.Number("1") || stdout["failed"] != json.Number("1") {
		t.Fatalf("archive child structured stdout = %#v", stdout)
	}
	results, ok := stdout["results"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("archive child per-item results = %#v", stdout["results"])
	}
	wantIDs := []string{"thread-ok", "thread-fail"}
	for i, raw := range results {
		item, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("archive child result[%d] type = %T, want JSON object", i, raw)
		}
		if item["threadId"] != wantIDs[i] {
			t.Fatalf("archive child result[%d] threadId = %#v, want %q", i, item["threadId"], wantIDs[i])
		}
		success, ok := item["success"].(bool)
		if !ok {
			t.Fatalf("archive child result[%d] success = %#v, want bool", i, item["success"])
		}
		_, hasError := item["error"]
		if i == 0 {
			if !success || hasError {
				t.Fatalf("archive child successful result[%d] = %#v, want success without error", i, item)
			}
			continue
		}
		errText, ok := item["error"].(string)
		if success || !hasError || !ok || !strings.Contains(errText, "synthetic thread failure") {
			t.Fatalf("archive child failed result[%d] = %#v, want failure with error", i, item)
		}
	}
}

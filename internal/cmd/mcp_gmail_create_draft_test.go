package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

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
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "gog-gmail-create-draft-test", Version: "1"}
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

func TestMCPGmailCreateDraftSchemaFieldsAndReplyCombinations(t *testing.T) {
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
		"gmail", "drafts", "create", "--to=to@example.com", "--cc=cc@example.com", "--bcc=bcc@example.com",
		"--subject=Subject", "--reply-to-message-id=m1", "--reply-to=reply@example.com", "--from=alias@example.com",
		"--body=" + body, "--body-html=" + html, "--reply-all", "--quote",
	})
	assertMCPGmailArgv(t, "gmail_create_draft", map[string]any{
		"thread_id": "thread-1", "body": "reply body", "reply_all": true, "quote": true,
	}, []string{
		"gmail", "drafts", "create", "--thread-id=thread-1", "--body=reply body", "--reply-all", "--quote",
	})
	assertMCPGmailArgv(t, "gmail_create_draft", map[string]any{
		"subject": "--option-shaped subject", "body": "---\nbody",
	}, []string{
		"gmail", "drafts", "create", "--subject=--option-shaped subject", "--body=---\nbody",
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

func TestMCPGmailCreateDraftInlineBodiesReachCLIUnchangedAndNeverSend(t *testing.T) {
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

func TestMCPGmailCreateDraftNoSendEndpoint(t *testing.T) {
	arguments := map[string]any{"to": "a@example.com", "subject": "Draft", "body": "hello"}
	svc, closeService := newGmailServiceForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages/send") || strings.HasSuffix(r.URL.Path, "/drafts/send") {
			t.Fatalf("create draft contacted send endpoint: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/users/me/drafts") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "d-nosend", "message": map[string]any{"id": "m-nosend"}})
	})
	defer closeService()

	result := runMCPGmailCLI(t, "gmail_create_draft", arguments, svc)
	if result.err != nil {
		t.Fatalf("create draft: %v\nstderr=%s", result.err, result.stderr)
	}
	out := decodeMCPGmailJSON(t, result.stdout)
	if out["draftId"] != "d-nosend" {
		t.Fatalf("structured create result = %#v", out)
	}
}

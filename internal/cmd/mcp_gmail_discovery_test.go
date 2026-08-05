package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"google.golang.org/api/gmail/v1"
)

// TestMCPGmailDiscoverySchemas defends the closed, typed input schemas of the
// three Gmail discovery tools: exact property sets, property types, defaults,
// integer bounds, required fields, and additionalProperties=false closure.
func TestMCPGmailDiscoverySchemas(t *testing.T) {
	tests := []struct {
		name       string
		toolName   string
		properties map[string]map[string]any
		required   []string
	}{
		{
			name:       "gmail_list_labels has no options",
			toolName:   "gmail_list_labels",
			properties: map[string]map[string]any{},
			required:   nil,
		},
		{
			name:     "gmail_list_drafts declares max and page_token",
			toolName: "gmail_list_drafts",
			properties: map[string]map[string]any{
				"max": {
					"type":    "integer",
					"default": float64(20),
					"minimum": float64(1),
					"maximum": float64(100),
				},
				"page_token": {"type": "string"},
			},
			required: nil,
		},
		{
			name:     "gmail_get_draft requires draft_id",
			toolName: "gmail_get_draft",
			properties: map[string]map[string]any{
				"draft_id": {"type": "string"},
			},
			required: []string{"draft_id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := findMCPTool(t, tt.toolName)
			if spec.Service != "gmail" || spec.Risk != mcpRiskRead {
				t.Fatalf("%s: service=%q risk=%q, want gmail/read", tt.toolName, spec.Service, spec.Risk)
			}
			tool := newMCPTool(spec)
			if got := tool.Name; got != tt.toolName {
				t.Fatalf("tool name = %q, want %q", got, tt.toolName)
			}

			if got, want := len(tool.InputSchema.Properties), len(tt.properties); got != want {
				t.Fatalf("%s: property count = %d, want %d (%#v)", tt.toolName, got, want, tool.InputSchema.Properties)
			}
			for key, wantSchema := range tt.properties {
				gotSchema, ok := tool.InputSchema.Properties[key]
				if !ok {
					t.Fatalf("%s: missing property %q", tt.toolName, key)
				}
				gotMap, ok := gotSchema.(map[string]any)
				if !ok {
					t.Fatalf("%s: property %q is %T, want map[string]any", tt.toolName, key, gotSchema)
				}
				for wantKey, wantVal := range wantSchema {
					if gotVal, present := gotMap[wantKey]; !present || !anyEqual(gotVal, wantVal) {
						t.Fatalf("%s.%s[%q] = %#v (%T), want %#v", tt.toolName, key, wantKey, gotVal, gotVal, wantVal)
					}
				}
				// Every property must carry a concrete JSON type.
				if _, ok := gotMap["type"].(string); !ok {
					t.Fatalf("%s.%s lacks a JSON type", tt.toolName, key)
				}
			}
			if got, want := fmtStrings(tool.InputSchema.Required), fmtStrings(tt.required); !stringSlicesEqual(got, want) {
				t.Fatalf("%s: required = %#v, want %#v", tt.toolName, got, want)
			}
			if tool.InputSchema.AdditionalProperties != false {
				t.Fatalf("%s: additionalProperties = %#v, want false (closed schema)", tt.toolName, tool.InputSchema.AdditionalProperties)
			}
		})
	}
}

// TestMCPGmailDiscoveryBuildArgs asserts the exact argv each tool produces for
// its typed arguments, including the one-page cursor (blank/absent token
// omits --page) and the no-attachment-download contract (never --download).
func TestMCPGmailDiscoveryBuildArgs(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		args     map[string]any
		want     []string
	}{
		{
			name:     "gmail_list_labels no options",
			toolName: "gmail_list_labels",
			args:     map[string]any{},
			want:     []string{"gmail", "labels", "list"},
		},
		{
			name:     "gmail_list_drafts default max",
			toolName: "gmail_list_drafts",
			args:     map[string]any{},
			want:     []string{"gmail", "drafts", "list", "--max", "20"},
		},
		{
			name:     "gmail_list_drafts custom max without token",
			toolName: "gmail_list_drafts",
			args:     map[string]any{"max": 5},
			want:     []string{"gmail", "drafts", "list", "--max", "5"},
		},
		{
			name:     "gmail_list_drafts preserves option-shaped opaque cursor",
			toolName: "gmail_list_drafts",
			args: map[string]any{
				"max":        5,
				"page_token": "--opaque-cursor-1",
			},
			want: []string{"gmail", "drafts", "list", "--max", "5", "--page=--opaque-cursor-1"},
		},
		{
			name:     "gmail_list_drafts blank token omits --page",
			toolName: "gmail_list_drafts",
			args: map[string]any{
				"max":        5,
				"page_token": "   ",
			},
			want: []string{"gmail", "drafts", "list", "--max", "5"},
		},
		{
			name:     "gmail_get_draft positional with -- separator",
			toolName: "gmail_get_draft",
			args:     map[string]any{"draft_id": "d1"},
			want:     []string{"gmail", "drafts", "get", "--", "d1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := findMCPTool(t, tt.toolName)
			got, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
				Arguments: tt.args,
			}})
			if err != nil {
				t.Fatalf("BuildArgs: %v", err)
			}
			if !stringSlicesEqual(got, tt.want) {
				t.Fatalf("argv = %#v, want %#v", got, tt.want)
			}
			// A discovery tool must never request attachment downloads.
			if strings.Contains(strings.Join(got, " "), "download") {
				t.Fatalf("unexpected download flag in argv: %#v", got)
			}
		})
	}
}

// TestMCPGmailDiscoveryBuildArgsRejectsEmptyDraftID ensures the required,
// non-empty draft_id is enforced at BuildArgs (the schema accepts an empty
// string as present, so closure is defended here).
func TestMCPGmailDiscoveryBuildArgsRejectsEmptyDraftID(t *testing.T) {
	tool := findMCPTool(t, "gmail_get_draft")
	for _, id := range []any{"", "   "} {
		_, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{
			Arguments: map[string]any{"draft_id": id},
		}})
		if err == nil {
			t.Fatalf("expected empty draft_id error, got nil")
		}
	}
}

// TestMCPGmailDiscoverySchemaRejection drives each tool through the in-process
// MCP server (with input-schema validation) and asserts invalid arguments are
// rejected before the handler runs, while a valid call reaches it.
func TestMCPGmailDiscoverySchemaRejection(t *testing.T) {
	cases := []struct {
		name     string
		toolName string
		valid    map[string]any
		invalid  []map[string]any
	}{
		{
			name:     "gmail_list_drafts",
			toolName: "gmail_list_drafts",
			valid:    map[string]any{"max": 5, "page_token": "abc"},
			invalid: []map[string]any{
				{"max": 101},               // above maximum
				{"max": 0},                 // below minimum
				{"max": "5"},               // wrong type
				{"max": 5, "unknown": "x"}, // unknown field
			},
		},
		{
			name:     "gmail_get_draft",
			toolName: "gmail_get_draft",
			valid:    map[string]any{"draft_id": "d1"},
			invalid: []map[string]any{
				{},
				{"draft_id": 123},                  // wrong type
				{"draft_id": "d1", "unknown": "x"}, // unknown field
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handlerCalls := 0
			s := newMCPServer()
			s.AddTool(newMCPTool(findMCPTool(t, tc.toolName)), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				handlerCalls++
				return mcp.NewToolResultText("ok"), nil
			})
			client := newInProcessMCPClient(t, s)
			defer client.Close()

			for _, bad := range tc.invalid {
				before := handlerCalls
				result := callMCPTool(t, client, tc.toolName, bad)
				if result == nil || !result.IsError {
					t.Fatalf("invalid args %#v accepted (IsError=%v)", bad, result)
				}
				if handlerCalls != before {
					t.Fatalf("invalid args %#v reached the tool handler", bad)
				}
			}

			before := handlerCalls
			okResult := callMCPTool(t, client, tc.toolName, tc.valid)
			if okResult == nil || okResult.IsError {
				t.Fatalf("valid args rejected: %#v", okResult)
			}
			if handlerCalls != before+1 {
				t.Fatalf("handler calls = %d, want %d", handlerCalls, before+1)
			}
		})
	}
}

// TestMCPGmailLabelsListProviderEnvelope runs the underlying command the
// gmail_list_labels tool delegates to, and asserts the provider envelope plus
// error propagation.
func TestMCPGmailLabelsListProviderEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/users/me/labels") || strings.HasSuffix(r.URL.Path, "/gmail/v1/users/me/labels") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"labels": []map[string]any{
					{"id": "INBOX", "name": "INBOX", "type": "system"},
					{"id": "Label_1", "name": "Custom", "type": "user"},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	svc := newGmailServiceFromServer(t, srv)
	flags := &RootFlags{Account: "a@b.com"}

	var out bytes.Buffer
	ctx := withGmailTestService(newCmdRuntimeJSONOutputContext(t, &out, io.Discard), svc)
	if err := runKong(t, &GmailLabelsListCmd{}, []string{}, ctx, flags); err != nil {
		t.Fatalf("execute: %v", err)
	}
	var envelope struct {
		Labels []*gmail.Label `json:"labels"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("json parse: %v\nout=%q", err, out.String())
	}
	if len(envelope.Labels) != 2 {
		t.Fatalf("labels envelope = %#v, want 2 entries", envelope.Labels)
	}
}

// TestMCPGmailListDraftsOnePageCursor runs the command gmail_list_drafts maps
// to and asserts opaque-cursor pass-through, a one-page fetch (a returned
// nextPageToken is surfaced, never auto-followed), and error propagation.
func TestMCPGmailListDraftsOnePageCursor(t *testing.T) {
	draftRequests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/gmail/v1/users/me/drafts") && r.Method == http.MethodGet {
			draftRequests++
			token := r.URL.Query().Get("pageToken")
			if token == "" {
				t.Fatal("expected pageToken query param on cursor page request")
			}
			if token != "opaque-cursor-1" {
				t.Fatalf("pageToken = %q, want opaque-cursor-1", token)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"drafts": []map[string]any{
					{"id": "d1", "message": map[string]any{"id": "m1", "threadId": "t1"}},
				},
				"nextPageToken": "next-cursor",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	svc := newGmailServiceFromServer(t, srv)
	flags := &RootFlags{Account: "a@b.com"}

	var out bytes.Buffer
	ctx := withGmailTestService(newCmdRuntimeJSONOutputContext(t, &out, io.Discard), svc)
	if err := runKong(t, &GmailDraftsListCmd{}, []string{"--max", "5", "--page", "opaque-cursor-1"}, ctx, flags); err != nil {
		t.Fatalf("execute: %v", err)
	}
	// One page, one call: the cursor is honoured, not followed.
	if draftRequests != 1 {
		t.Fatalf("draft list calls = %d, want 1 (one page per call)", draftRequests)
	}
	var envelope struct {
		Drafts []struct {
			ID string `json:"id"`
		} `json:"drafts"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("json parse: %v\nout=%q", err, out.String())
	}
	if len(envelope.Drafts) != 1 || envelope.NextPageToken != "next-cursor" {
		t.Fatalf("drafts envelope = %#v", envelope)
	}
}

// TestMCPGmailGetDraftNoAttachmentDownload runs the command gmail_get_draft
// maps to (without --download) and asserts that an attachment-bearing draft
// is returned without any attachment-byte request.
func TestMCPGmailGetDraftNoAttachmentDownload(t *testing.T) {
	attachmentRequests := 0
	payloadText := encodeGmailBase64(t, "Draft body")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/attachments/"):
			attachmentRequests++
			http.NotFound(w, r)
		case strings.Contains(r.URL.Path, "/gmail/v1/users/me/drafts/d1") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "d1",
				"message": map[string]any{
					"id": "m1",
					"payload": map[string]any{
						"mimeType": "multipart/mixed",
						"headers":  []map[string]any{{"name": "Subject", "value": "Draft"}},
						"parts": []map[string]any{
							{"mimeType": "text/plain", "body": map[string]any{"data": payloadText}},
							{"filename": "file.txt", "mimeType": "text/plain", "body": map[string]any{"attachmentId": "att1", "size": 10}},
						},
					},
				},
			})
			return
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc := newGmailServiceFromServer(t, srv)
	flags := &RootFlags{Account: "a@b.com"}

	var out bytes.Buffer
	ctx := withGmailTestService(newCmdRuntimeJSONOutputContext(t, &out, io.Discard), svc)
	if err := runKong(t, &GmailDraftsGetCmd{}, []string{"d1"}, ctx, flags); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if attachmentRequests != 0 {
		t.Fatalf("attachment requests = %d, want 0 (no attachment download without Download flag)", attachmentRequests)
	}
	var envelope struct {
		Draft *gmail.Draft `json:"draft"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("json parse: %v\nout=%q", err, out.String())
	}
	if envelope.Draft == nil || envelope.Draft.Id != "d1" {
		t.Fatalf("draft envelope = %#v", envelope.Draft)
	}
}

// TestMCPGmailDiscoveryErrorPropagation asserts provider failures surface as
// command errors (without asserting on source text) for all three commands
// the discovery tools delegate to.
func TestMCPGmailDiscoveryErrorPropagation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "provider exploded", http.StatusInternalServerError)
	}))
	defer srv.Close()

	svc := newGmailServiceFromServer(t, srv)
	flags := &RootFlags{Account: "a@b.com"}

	tests := []struct {
		name string
		run  func(t *testing.T) error
	}{
		{
			name: "gmail labels list",
			run: func(t *testing.T) error {
				t.Helper()
				ctx := withGmailTestService(newCmdRuntimeJSONOutputContext(t, io.Discard, io.Discard), svc)
				return runKong(t, &GmailLabelsListCmd{}, []string{}, ctx, flags)
			},
		},
		{
			name: "gmail drafts list",
			run: func(t *testing.T) error {
				t.Helper()
				ctx := withGmailTestService(newCmdRuntimeJSONOutputContext(t, io.Discard, io.Discard), svc)
				return runKong(t, &GmailDraftsListCmd{}, []string{}, ctx, flags)
			},
		},
		{
			name: "gmail drafts get",
			run: func(t *testing.T) error {
				t.Helper()
				ctx := withGmailTestService(newCmdRuntimeJSONOutputContext(t, io.Discard, io.Discard), svc)
				return runKong(t, &GmailDraftsGetCmd{}, []string{"d1"}, ctx, flags)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(t); err == nil {
				t.Fatalf("expected provider error to propagate, got nil")
			}
		})
	}
}

// --- helpers ---

func encodeGmailBase64(t *testing.T, s string) string {
	t.Helper()
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func newInProcessMCPClient(t *testing.T, s *server.MCPServer) *mcpclient.Client {
	t.Helper()
	client, err := mcpclient.NewInProcessClient(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "gog-test", Version: "1"}
	if _, err := client.Initialize(t.Context(), initRequest); err != nil {
		t.Fatal(err)
	}
	return client
}

func callMCPTool(t *testing.T, client *mcpclient.Client, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := client.CallTool(t.Context(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: name, Arguments: args},
	})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return result
}

func fmtStrings(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func anyEqual(a, b any) bool {
	if af, aok := asFloat(a); aok {
		if bf, bok := asFloat(b); bok {
			return af == bf
		}
	}
	switch sa := a.(type) {
	case string:
		sb, ok := b.(string)
		return ok && sa == sb
	case bool:
		sb, ok := b.(bool)
		return ok && sa == sb
	default:
		return fmt.Sprintf("%#v", a) == fmt.Sprintf("%#v", b)
	}
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

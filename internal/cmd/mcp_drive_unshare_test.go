package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"google.golang.org/api/drive/v3"
)

func TestMCPDriveUnshareBuildArgsExact(t *testing.T) {
	tool := mcpDriveUnshareTool()
	for name, args := range map[string]map[string]any{
		"ordinary IDs":     {"file_id": "file-1", "permission_id": "permission-7"},
		"leading dash IDs": {"file_id": "--file-1", "permission_id": "--permission-7"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
			if err != nil {
				t.Fatalf("BuildArgs: %v", err)
			}
			want := []string{"drive", "unshare", "--force", "--", args["file_id"].(string), args["permission_id"].(string)}
			if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
				t.Fatalf("argv = %#v, want %#v", got, want)
			}
		})
	}

	for name, args := range map[string]map[string]any{
		"missing file ID":       {"permission_id": "permission-7"},
		"missing permission ID": {"file_id": "file-1"},
		"empty file ID":         {"file_id": "   ", "permission_id": "permission-7"},
		"empty permission ID":   {"file_id": "file-1", "permission_id": "  "},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}}); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestMCPDriveUnshareSchemaIsClosedAndExact(t *testing.T) {
	tool := mcpDriveUnshareTool()
	if tool.Service != "drive" || tool.Risk != mcpRiskDestructive {
		t.Fatalf("tool metadata = service %q risk %q", tool.Service, tool.Risk)
	}
	schema := newMCPTool(tool).InputSchema
	if closed, ok := schema.AdditionalProperties.(bool); !ok || closed {
		t.Fatalf("schema additionalProperties = %#v, want false", schema.AdditionalProperties)
	}
	if len(schema.Required) != 2 || schema.Required[0] != "file_id" || schema.Required[1] != "permission_id" {
		t.Fatalf("required fields = %#v, want [file_id permission_id]", schema.Required)
	}
	if len(schema.Properties) != 2 {
		t.Fatalf("schema properties = %#v, want exactly file_id and permission_id", schema.Properties)
	}
	for _, field := range []string{"file_id", "permission_id"} {
		if _, ok := schema.Properties[field]; !ok {
			t.Fatalf("schema missing %q", field)
		}
	}
	for _, field := range []string{
		"force", "share", "action", "path", "generic", "target", "permission", "permissions", "query", "email", "role", "argv", "args", "host_path",
	} {
		if _, ok := schema.Properties[field]; ok {
			t.Fatalf("schema exposes forbidden field %q", field)
		}
	}
}

func TestMCPDriveUnsharePolicyRequiresExplicitDestructiveSelection(t *testing.T) {
	for _, cmd := range []McpCmd{
		{},
		{AllowTool: []string{"read"}},
		{AllowWrite: true, AllowTool: []string{"write"}},
		{AllowWrite: true, AllowTool: []string{"drive"}},
		{AllowWrite: true, AllowTool: []string{"drive.*"}},
		{AllowWrite: true, AllowTool: []string{"all"}},
	} {
		if hasMCPTool(mcpEnabledTools(cmd), "drive_unshare") {
			t.Fatalf("selector %#v unexpectedly exposed drive_unshare", cmd)
		}
	}
	for _, selector := range []string{"destructive", "drive_unshare"} {
		cmd := McpCmd{AllowWrite: true, AllowTool: []string{selector}}
		if !hasMCPTool(mcpEnabledTools(cmd), "drive_unshare") {
			t.Fatalf("selector %q did not expose explicitly authorized drive_unshare", selector)
		}
	}
}

func TestDriveUnshareV02ReadBeforeMutationHTTPFixture(t *testing.T) {
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/drive/v3")
		switch {
		case r.Method == http.MethodGet && path == "/files/file-1/permissions":
			requests = append(requests, r.Method+" "+path)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"permissions":[{"id":"permission-7","type":"user","role":"reader","emailAddress":"fixture@example.test"}]}`)
		case r.Method == http.MethodDelete && path == "/files/file-1/permissions/permission-7":
			if len(requests) != 1 || requests[0] != "GET /files/file-1/permissions" {
				t.Fatalf("permission mutation happened before V02 read: %#v", requests)
			}
			if r.URL.Query().Get("supportsAllDrives") != "true" {
				t.Fatalf("supportsAllDrives = %q, want true", r.URL.Query().Get("supportsAllDrives"))
			}
			requests = append(requests, r.Method+" "+path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected Drive request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer srv.Close()
	svc := newGoogleTestServiceWithEndpoint(t, srv.Client(), srv.URL+"/", drive.NewService)

	var readOut bytes.Buffer
	readCtx := withDriveTestService(newCmdRuntimeJSONOutputContext(t, &readOut, io.Discard), svc)
	if err := runKong(t, &DrivePermissionsCmd{}, []string{"file-1"}, readCtx, &RootFlags{Account: "fixture@example.test"}); err != nil {
		t.Fatalf("V02 permissions read: %v", err)
	}
	var permissionRead struct {
		Permissions []struct {
			ID string `json:"id"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(readOut.Bytes(), &permissionRead); err != nil {
		t.Fatalf("decode V02 permissions result: %v", err)
	}
	if len(permissionRead.Permissions) != 1 || permissionRead.Permissions[0].ID != "permission-7" {
		t.Fatalf("V02 permissions result = %#v", permissionRead)
	}

	var deleteOut bytes.Buffer
	deleteCtx := withDriveTestService(newCmdRuntimeJSONOutputContext(t, &deleteOut, io.Discard), svc)
	if err := runKong(t, &DriveUnshareCmd{}, []string{"file-1", permissionRead.Permissions[0].ID}, deleteCtx, &RootFlags{Account: "fixture@example.test", Force: true}); err != nil {
		t.Fatalf("exact permission removal: %v", err)
	}
	if len(requests) != 2 || requests[1] != "DELETE /files/file-1/permissions/permission-7" {
		t.Fatalf("request sequence = %#v", requests)
	}
}

func TestMCPDriveUnshareServerDryRunStructuredChild(t *testing.T) {
	t.Setenv("GOG_MCP_X06_DRIVE_UNSHARE_HELPER", "1")
	tool := mcpDriveUnshareTool()
	commandArgs, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
		"file_id":       "file-1",
		"permission_id": "permission-7",
	}}})
	if err != nil {
		t.Fatalf("BuildArgs: %v", err)
	}
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self: os.Args[0],
		tool: tool,
		baseArgs: []string{
			"-test.run=TestMCPDriveUnshareServerDryRunStructuredChildHelper$", "--",
			"--json", "--wrap-untrusted", "--no-input", "--dry-run",
		},
		commandArgs:    commandArgs,
		timeout:        5 * time.Second,
		maxOutputBytes: 4096,
	})
	got := requireMCPNativeCommandResult(t, result)
	if result.IsError || got.ExitCode != 0 || got.Stderr != "" {
		t.Fatalf("dry-run child result = %#v", got)
	}
	stdout, ok := got.Stdout.(map[string]any)
	if !ok {
		t.Fatalf("structured child stdout type = %T, value=%#v", got.Stdout, got.Stdout)
	}
	if stdout["removed"] != true || stdout["fileId"] != "file-1" || stdout["permissionId"] != "permission-7" {
		t.Fatalf("structured child stdout = %#v", stdout)
	}
}

func TestMCPDriveUnshareServerDryRunStructuredChildHelper(t *testing.T) {
	if os.Getenv("GOG_MCP_X06_DRIVE_UNSHARE_HELPER") != "1" {
		return
	}
	joined := strings.Join(os.Args[1:], "\x00")
	want := "\x00--dry-run\x00drive\x00unshare\x00--force\x00--\x00file-1\x00permission-7"
	if !strings.HasSuffix(joined, want) {
		os.Exit(2)
	}
	_, _ = io.WriteString(os.Stdout, `{"removed":true,"fileId":"file-1","permissionId":"permission-7"}`)
	os.Exit(0)
}

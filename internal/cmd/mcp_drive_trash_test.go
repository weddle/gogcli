package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"google.golang.org/api/drive/v3"
)

func TestMCPDriveTrashBuildArgsExact(t *testing.T) {
	tool := mcpDriveTrashTool()
	for _, tt := range []struct {
		name string
		args map[string]any
		want []string
	}{
		{
			name: "file ID",
			args: map[string]any{"file_id": "file-1"},
			want: []string{"drive", "delete", "--force", "--", "file-1"},
		},
		{
			name: "leading dash remains positional",
			args: map[string]any{"file_id": " --file-id "},
			want: []string{"drive", "delete", "--force", "--", "--file-id"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tt.args}})
			if err != nil {
				t.Fatalf("BuildArgs: %v", err)
			}
			if strings.Join(got, "\x00") != strings.Join(tt.want, "\x00") {
				t.Fatalf("argv = %#v, want %#v", got, tt.want)
			}
			for _, forbidden := range []string{"--permanent", "--path", "--out", "@file"} {
				if strings.Contains(strings.Join(got, "\x00"), forbidden) {
					t.Fatalf("argv contains forbidden input %q: %#v", forbidden, got)
				}
			}
		})
	}
	for _, tt := range []struct {
		name string
		args map[string]any
	}{
		{name: "missing file ID", args: map[string]any{}},
		{name: "empty file ID", args: map[string]any{"file_id": " \t"}},
		{name: "wrong file ID type", args: map[string]any{"file_id": 42}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tt.args}}); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestMCPDriveTrashSchemaAndRisk(t *testing.T) {
	tool := mcpDriveTrashTool()
	if tool.Name != "drive_trash" || tool.Service != "drive" || tool.Risk != mcpRiskDestructive {
		t.Fatalf("tool metadata = %#v", tool)
	}
	schema := newMCPTool(tool).InputSchema
	if closed, ok := schema.AdditionalProperties.(bool); !ok || closed {
		t.Fatalf("schema additionalProperties = %#v, want false", schema.AdditionalProperties)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "file_id" {
		t.Fatalf("required fields = %#v, want [file_id]", schema.Required)
	}
	if len(schema.Properties) != 1 {
		t.Fatalf("schema properties = %#v, want only file_id", schema.Properties)
	}
	if _, ok := schema.Properties["file_id"]; !ok {
		t.Fatal("schema missing file_id")
	}
	for _, field := range []string{
		"permanent", "force", "path", "local_path", "host_path", "out", "output", "stdin", "argv", "args", "file",
	} {
		if _, ok := schema.Properties[field]; ok {
			t.Fatalf("schema exposes forbidden field %q", field)
		}
	}
}

func TestMCPDriveTrashRequiresExplicitDestructivePolicy(t *testing.T) {
	selectors := []struct {
		name        string
		cmd         McpCmd
		wantVisible bool
	}{
		{name: "default", cmd: McpCmd{}},
		{name: "write authorization only", cmd: McpCmd{AllowWrite: true}},
		{name: "read selector", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"read"}}},
		{name: "write selector", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"write"}}},
		{name: "drive selector", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"drive"}}},
		{name: "drive wildcard", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"drive.*"}}},
		{name: "all selector", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"all"}}},
		{name: "destructive selector", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"destructive"}}, wantVisible: true},
		{name: "exact selector", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"drive_trash"}}, wantVisible: true},
		{name: "exact without write", cmd: McpCmd{AllowTool: []string{"drive_trash"}}},
	}
	for _, tt := range selectors {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasMCPTool(mcpEnabledTools(tt.cmd), "drive_trash"); got != tt.wantVisible {
				t.Fatalf("visible = %t, want %t; tools=%#v", got, tt.wantVisible, toolNames(mcpEnabledTools(tt.cmd)))
			}
		})
	}
	if hasMCPTool(mcpEnabledToolsNoPolicy(McpCmd{AllowWrite: true, AllowTool: []string{"destructive"}}, &RootFlags{ReadOnly: true}), "drive_trash") {
		t.Fatal("readonly runtime exposed drive_trash")
	}
}

func TestMCPDriveTrashUsesDefaultDriveDeleteOverHTTP(t *testing.T) {
	var patchCount, deleteCount int
	svc, closeSrv := newDriveTestService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/files/file-1") {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodPatch, http.MethodPut:
			patchCount++
			requireSupportsAllDrives(t, r)
			if body := readBody(t, r); !strings.Contains(body, `"trashed":true`) {
				t.Fatalf("default trash body = %q", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"file-1","trashed":true}`)
		case http.MethodDelete:
			deleteCount++
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer closeSrv()

	args, err := mcpDriveTrashTool().BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
		"file_id": "file-1",
	}}})
	if err != nil {
		t.Fatalf("BuildArgs: %v", err)
	}
	result := executeWithDriveTestService(t, append([]string{"--json", "--account", "a@b.com"}, args...), svc)
	if result.err != nil {
		t.Fatalf("execute generated argv: %v\nstdout=%s\nstderr=%s", result.err, result.stdout, result.stderr)
	}
	var got struct {
		Trashed bool   `json:"trashed"`
		Deleted bool   `json:"deleted"`
		ID      string `json:"id"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &got); err != nil {
		t.Fatalf("decode result: %v\nstdout=%s", err, result.stdout)
	}
	if !got.Trashed || got.Deleted || got.ID != "file-1" {
		t.Fatalf("result = %#v", got)
	}
	if patchCount != 1 || deleteCount != 0 {
		t.Fatalf("provider methods: patch=%d delete=%d", patchCount, deleteCount)
	}
}

func TestMCPDriveTrashDryRunSkipsDriveService(t *testing.T) {
	args, err := mcpDriveTrashTool().BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
		"file_id": "file-1",
	}}})
	if err != nil {
		t.Fatalf("BuildArgs: %v", err)
	}
	result := executeWithDriveTestServiceFactory(t, append([]string{"--json", "--dry-run", "--no-input", "--account", "a@b.com"}, args...), func(_ context.Context, _ string) (*drive.Service, error) {
		t.Fatal("Drive service should not be called during dry-run")
		return nil, errUnexpectedDriveServiceCall
	})
	if result.err != nil {
		t.Fatalf("dry-run execute: %v\nstderr=%s", result.err, result.stderr)
	}
	var got struct {
		DryRun  bool           `json:"dry_run"`
		Op      string         `json:"op"`
		Request map[string]any `json:"request"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &got); err != nil {
		t.Fatalf("decode dry-run: %v\nstdout=%s", err, result.stdout)
	}
	if !got.DryRun || got.Op != "drive.delete" || got.Request["file_id"] != "file-1" || got.Request["permanent"] != false {
		t.Fatalf("dry-run envelope = %#v", got)
	}
}

func TestMCPDriveTrashRunnerReturnsStructuredChildResult(t *testing.T) {
	t.Setenv("GOG_MCP_X04_DRIVE_TRASH_HELPER", "1")
	tool := mcpDriveTrashTool()
	args, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
		"file_id": "file-1",
	}}})
	if err != nil {
		t.Fatalf("BuildArgs: %v", err)
	}
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self: os.Args[0],
		tool: tool,
		baseArgs: []string{
			"-test.run=TestMCPDriveTrashRunnerHelper$", "--",
			"--json", "--wrap-untrusted", "--no-input", "--color=never", "--dry-run",
		},
		commandArgs:    args,
		timeout:        5 * time.Second,
		maxOutputBytes: 4096,
	})
	if result.IsError {
		t.Fatalf("runner result = %#v", result.StructuredContent)
	}
	got, ok := result.StructuredContent.(mcpCommandResult)
	if !ok {
		t.Fatalf("structured result type = %T, value=%#v", result.StructuredContent, result.StructuredContent)
	}
	if got.Tool != "drive_trash" || got.Service != "drive" || got.Risk != string(mcpRiskDestructive) || got.ExitCode != 0 || got.Stderr != "" {
		t.Fatalf("runner envelope = %#v", got)
	}
	stdout, ok := got.Stdout.(map[string]any)
	if !ok || stdout["id"] != "file-1" || stdout["trashed"] != true || stdout["deleted"] != false {
		t.Fatalf("runner stdout = %#v", got.Stdout)
	}
}

func TestMCPDriveTrashRunnerHelper(t *testing.T) {
	if os.Getenv("GOG_MCP_X04_DRIVE_TRASH_HELPER") != "1" {
		return
	}
	joined := "\x00" + strings.Join(os.Args[1:], "\x00") + "\x00"
	for _, required := range []string{
		"\x00--json\x00", "\x00--no-input\x00", "\x00--dry-run\x00",
		"\x00drive\x00delete\x00--force\x00--\x00file-1\x00",
	} {
		if !strings.Contains(joined, required) {
			os.Exit(2)
		}
	}
	for _, forbidden := range []string{"\x00--permanent\x00", "\x00--path\x00", "\x00--out\x00", "\x00@file\x00"} {
		if strings.Contains(joined, forbidden) {
			os.Exit(2)
		}
	}
	_, _ = io.WriteString(os.Stdout, `{"id":"file-1","trashed":true,"deleted":false}`)
	os.Exit(0)
}

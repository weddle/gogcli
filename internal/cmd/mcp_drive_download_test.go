package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"google.golang.org/api/drive/v3"
)

func TestMCPDriveDownloadBuildArgsExact(t *testing.T) {
	tool := mcpDriveDownloadTool()
	tests := []struct {
		name string
		args map[string]any
		want []string
	}{
		{
			name: "inferred export",
			args: map[string]any{"file_id": "file-1"},
			want: []string{"drive", "download", "--max-bytes", "65536", "--out", "-", "--", "file-1"},
		},
		{
			name: "explicit format",
			args: map[string]any{"file_id": "file-1", "format": "pdf"},
			want: []string{"drive", "download", "--max-bytes", "65536", "--out", "-", "--format", "pdf", "--", "file-1"},
		},
		{
			name: "leading dash ID remains positional",
			args: map[string]any{"file_id": "--file-id", "format": "txt"},
			want: []string{"drive", "download", "--max-bytes", "65536", "--out", "-", "--format", "txt", "--", "--file-id"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tt.args}})
			if err != nil {
				t.Fatalf("BuildArgs: %v", err)
			}
			if strings.Join(got, "\x00") != strings.Join(tt.want, "\x00") {
				t.Fatalf("argv = %#v, want %#v", got, tt.want)
			}
		})
	}

	for name, args := range map[string]map[string]any{
		"missing file ID": {"format": "pdf"},
		"empty file ID":   {"file_id": "   "},
		"empty format":    {"file_id": "file-1", "format": "  "},
		"invalid format":  {"file_id": "file-1", "format": "zip"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}}); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestMCPDriveDownloadSchemaIsClosedAndReadOnly(t *testing.T) {
	tool := mcpDriveDownloadTool()
	if tool.Risk != mcpRiskRead || tool.Service != "drive" {
		t.Fatalf("tool metadata = service %q risk %q", tool.Service, tool.Risk)
	}
	schema := newMCPTool(tool).InputSchema
	if closed, ok := schema.AdditionalProperties.(bool); !ok || closed {
		t.Fatalf("schema additionalProperties = %#v, want false", schema.AdditionalProperties)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "file_id" {
		t.Fatalf("required fields = %#v, want [file_id]", schema.Required)
	}
	if len(schema.Properties) != 2 {
		t.Fatalf("schema properties = %#v, want exactly file_id and format", schema.Properties)
	}
	for _, field := range []string{"file_id", "format"} {
		if _, ok := schema.Properties[field]; !ok {
			t.Fatalf("schema missing %q", field)
		}
	}
	for _, field := range []string{
		"max_bytes", "tab", "out", "output", "overwrite", "path", "host_path", "stdin", "argv", "args", "resource", "resource_uri",
	} {
		if _, ok := schema.Properties[field]; ok {
			t.Fatalf("schema exposes forbidden field %q", field)
		}
	}
}

func TestMCPDriveDownloadReadPolicy(t *testing.T) {
	for _, cmd := range []McpCmd{
		{},
		{AllowTool: []string{"read"}},
		{AllowTool: []string{"drive"}},
		{AllowTool: []string{"drive.*"}},
		{AllowWrite: true, AllowTool: []string{"all"}},
	} {
		if !hasMCPTool(mcpEnabledTools(cmd), "drive_download") {
			t.Fatalf("selector %#v omitted drive_download", cmd.AllowTool)
		}
	}
	if hasMCPTool(mcpEnabledTools(McpCmd{AllowWrite: true, AllowTool: []string{"write"}}), "drive_download") {
		t.Fatal("ordinary write selector unexpectedly changed read exposure")
	}
}

func TestMCPDriveDownloadOutputMetadata(t *testing.T) {
	name, mimeType, err := mcpDriveDownloadOutputMetadata(&drive.File{
		Name:     "reports/quarterly",
		MimeType: driveMimeGoogleDoc,
	}, "txt")
	if err != nil {
		t.Fatalf("output metadata: %v", err)
	}
	if name != "reports/quarterly.txt" || mimeType != mimeTextPlain {
		t.Fatalf("metadata = name %q mime %q", name, mimeType)
	}

	name, mimeType, err = mcpDriveDownloadOutputMetadata(&drive.File{Name: "raw.bin", MimeType: "application/octet-stream"}, "")
	if err != nil {
		t.Fatalf("raw metadata: %v", err)
	}
	if name != "raw.bin" || mimeType != "application/octet-stream" {
		t.Fatalf("raw metadata = name %q mime %q", name, mimeType)
	}
	if _, _, err := mcpDriveDownloadOutputMetadata(&drive.File{Name: "raw.bin", MimeType: "application/octet-stream"}, "pdf"); err == nil {
		t.Fatal("expected format rejection for non-Workspace file")
	}
}

func TestMCPDriveDownloadMetadataUsesUnwrappedDriveGetJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/files/file-1") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"file-1","name":"Quarterly Report","mimeType":"application/pdf"}`)
	}))
	defer srv.Close()

	svc := newGoogleTestServiceWithEndpoint(t, srv.Client(), srv.URL+"/", drive.NewService)
	var stdout bytes.Buffer
	ctx := withDriveTestService(newCmdRuntimeJSONOutputContext(t, &stdout, io.Discard), svc)
	if err := runKong(t, &DriveGetCmd{}, []string{"file-1", "--fields", "id,name,mimeType"}, ctx, &RootFlags{Account: "reader@example.com"}); err != nil {
		t.Fatalf("drive get: %v", err)
	}
	child := mcp.NewToolResultStructuredOnly(mcpCommandResult{
		Tool:     "drive_get",
		Service:  "drive",
		Risk:     string(mcpRiskRead),
		Stdout:   parseMCPStdout(stdout.String()),
		ExitCode: 0,
	})
	metadata, err := mcpDriveDownloadMetadata(child, "file-1")
	if err != nil {
		t.Fatalf("parse metadata: %v", err)
	}
	if metadata.Name != "Quarterly Report" || metadata.MimeType != "application/pdf" {
		t.Fatalf("metadata = %#v, want unwrapped provider fields", metadata)
	}
}

func TestMCPDriveDownloadInlineResultBoundariesAndEnvelope(t *testing.T) {
	for _, size := range []int{mcpInlineBinaryMaxBytes - 1, mcpInlineBinaryMaxBytes, mcpInlineBinaryMaxBytes + 1} {
		size := size
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			raw := bytes.Repeat([]byte("x"), size)
			result := mcpInlineBinaryToolResult(raw, "folder/report.bin", "application/octet-stream", 200000)
			if size > mcpInlineBinaryMaxBytes {
				if !result.IsError {
					t.Fatalf("oversize result = %#v, want error", result.StructuredContent)
				}
				errorObject, ok := result.StructuredContent.(mcpInlineBinaryError)
				if !ok || errorObject.Error != "binary_size_limit" || errorObject.LimitBytes != mcpInlineBinaryMaxBytes {
					t.Fatalf("oversize error = %#v", result.StructuredContent)
				}
				return
			}
			content, ok := result.StructuredContent.(mcpInlineBinaryContent)
			if !ok {
				t.Fatalf("structured content = %T, want B03 content", result.StructuredContent)
			}
			if content.Name != "report.bin" || content.MimeType != "application/octet-stream" || content.Size != size {
				t.Fatalf("content metadata = %#v", content)
			}
			decoded, err := base64.StdEncoding.DecodeString(content.ContentBase64)
			if err != nil || !bytes.Equal(decoded, raw) {
				t.Fatalf("decoded content mismatch: err=%v size=%d", err, len(decoded))
			}
			if len(content.ContentBase64)%4 != 0 {
				t.Fatal("contentBase64 is not padded standard base64")
			}
		})
	}

	if result := mcpInlineBinaryToolResult([]byte("data"), "file", "", 200000); !result.IsError {
		t.Fatal("invalid MIME should fail")
	}
	if result := mcpInlineBinaryToolResult([]byte("data"), "file", "text/plain", 1); !result.IsError {
		t.Fatal("output envelope cap should fail without partial content")
	}
}

func TestMCPDriveDownloadRawCaptureIsBounded(t *testing.T) {
	capture := mcpDriveDownloadRawCapture{limit: 3}
	if n, err := capture.Write([]byte("abcd")); err != nil || n != 4 {
		t.Fatalf("capture write = n %d err %v", n, err)
	}
	if got := capture.String(); got != "abc" || !capture.overflow {
		t.Fatalf("capture = %q overflow=%t", got, capture.overflow)
	}
}

func TestMCPDriveDownloadRunnerUsesMetadataAndRawChildren(t *testing.T) {
	t.Setenv("GOG_MCP_B04_DRIVE_DOWNLOAD_HELPER", "1")
	t.Setenv("GOG_MCP_B04_DRIVE_DOWNLOAD_SIZE", "4")
	tool := findMCPTool(t, "drive_download")
	commandArgs, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
		"file_id": "file-1",
		"format":  "txt",
	}}})
	if err != nil {
		t.Fatalf("BuildArgs: %v", err)
	}
	result := mcpRunDriveDownload(t.Context(), mcpRunOptions{
		self: os.Args[0],
		tool: tool,
		baseArgs: []string{
			"-test.run=TestMCPDriveDownloadRunnerHelper$", "--",
			"--json", "--wrap-untrusted", "--results-only", "--select", "name", "--dry-run",
		},
		commandArgs:    commandArgs,
		timeout:        5 * time.Second,
		maxOutputBytes: 200000,
	})
	if result.IsError {
		t.Fatalf("drive_download runner error: %#v", result.StructuredContent)
	}
	content, ok := result.StructuredContent.(mcpInlineBinaryContent)
	if !ok {
		t.Fatalf("runner content = %T %#v", result.StructuredContent, result.StructuredContent)
	}
	if content.Name != "report.txt" || content.MimeType != mimeTextPlain || content.Size != 4 {
		t.Fatalf("runner metadata = %#v", content)
	}
	decoded, err := base64.StdEncoding.DecodeString(content.ContentBase64)
	if err != nil || string(decoded) != "xxxx" {
		t.Fatalf("runner decoded content = %q err=%v", decoded, err)
	}
}

func TestMCPDriveDownloadRunnerHelper(t *testing.T) {
	if os.Getenv("GOG_MCP_B04_DRIVE_DOWNLOAD_HELPER") != "1" {
		return
	}
	joined := strings.Join(os.Args[1:], "\x00")
	for _, forbidden := range []string{"--wrap-untrusted", "--results-only", "--select", "--dry-run"} {
		if strings.Contains(joined, "\x00"+forbidden) {
			os.Exit(2)
		}
	}
	if strings.Contains(joined, "\x00drive\x00get\x00") {
		if !strings.Contains(joined, "\x00--fields\x00id,name,mimeType\x00--\x00file-1") || !strings.Contains(joined, "\x00--json") {
			os.Exit(2)
		}
		_, _ = io.WriteString(os.Stdout, `{"id":"file-1","name":"report","mimeType":"application/vnd.google-apps.document"}`)
		os.Exit(0)
	}
	if strings.Contains(joined, "\x00drive\x00download\x00") {
		if strings.Contains(joined, "\x00--json") || !strings.Contains(joined, "\x00--max-bytes\x0065536\x00--out\x00-\x00--format\x00txt\x00--\x00file-1") {
			os.Exit(2)
		}
		size, err := strconv.Atoi(os.Getenv("GOG_MCP_B04_DRIVE_DOWNLOAD_SIZE"))
		if err != nil {
			os.Exit(2)
		}
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), size))
		os.Exit(0)
	}
	os.Exit(2)
}

func TestMCPDriveDownloadMetadataRejectsMalformedChildResult(t *testing.T) {
	result := mcp.NewToolResultStructuredOnly(mcpCommandResult{
		Tool:     "drive_get",
		Service:  "drive",
		Risk:     string(mcpRiskRead),
		ExitCode: 0,
		Stdout:   json.RawMessage(`[]`),
	})
	if _, err := mcpDriveDownloadMetadata(result, "file-1"); err == nil {
		t.Fatal("expected malformed metadata error")
	}
}

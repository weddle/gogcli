package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"google.golang.org/api/drive/v3"

	"github.com/steipete/gogcli/internal/app"
)

func TestMCPM11DriveSearchBuildArgsSharedDrive(t *testing.T) {
	tool := findMCPTool(t, "drive_search")
	tests := []struct {
		name      string
		arguments map[string]any
		want      []string
	}{
		{
			name: "shared drive selector",
			arguments: map[string]any{
				"query":    "report",
				"drive_id": " 0AFakeSharedDriveID ",
			},
			want: []string{"drive", "search", "--max", "20", "--drive", "0AFakeSharedDriveID", "--", "report"},
		},
		{
			name: "shared drive and parent",
			arguments: map[string]any{
				"query":    "report",
				"parent":   " folder-123 ",
				"drive_id": " drive-123 ",
			},
			want: []string{"drive", "search", "--max", "20", "--parent", "folder-123", "--drive", "drive-123", "--", "report"},
		},
		{
			name: "shared drive query delimiter",
			arguments: map[string]any{
				"query":    "--drive",
				"drive_id": "drive-123",
			},
			want: []string{"drive", "search", "--max", "20", "--drive", "drive-123", "--", "--drive"},
		},
		{
			name:      "empty selector omitted",
			arguments: map[string]any{"query": "report", "drive_id": " \t "},
			want:      []string{"drive", "search", "--max", "20", "--", "report"},
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

func TestMCPM11DriveSearchSchemaIsClosedAndTyped(t *testing.T) {
	tests := []struct {
		name      string
		arguments map[string]any
		wantText  string
	}{
		{name: "unknown page token", arguments: map[string]any{"query": "report", "page_token": "next"}, wantText: "page_token"},
		{name: "unknown all drives toggle", arguments: map[string]any{"query": "report", "all_drives": false}, wantText: "all_drives"},
		{name: "unknown generic argv", arguments: map[string]any{"query": "report", "argv": []any{"drive", "search"}}, wantText: "argv"},
		{name: "wrong drive id type", arguments: map[string]any{"query": "report", "drive_id": 42}, wantText: "drive_id"},
		{name: "wrong query type", arguments: map[string]any{"query": 42}, wantText: "query"},
		{name: "missing query", arguments: map[string]any{}, wantText: "query"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, handlerCalls := callMCPWaveANativeSchema(t, "drive_search", tt.arguments)
			if !result.IsError {
				t.Fatalf("schema accepted invalid arguments: %#v", tt.arguments)
			}
			if handlerCalls != 0 {
				t.Fatalf("invalid arguments reached handler: %d calls", handlerCalls)
			}
			if text := mcpResultText(result); !strings.Contains(text, tt.wantText) {
				t.Fatalf("schema error = %q, want %q", text, tt.wantText)
			}
		})
	}

	result, handlerCalls := callMCPWaveANativeSchema(t, "drive_search", map[string]any{
		"query":    "report",
		"drive_id": "drive-123",
	})
	if result.IsError || handlerCalls != 1 {
		t.Fatalf("valid shared-drive input result=%#v handlerCalls=%d", result.Content, handlerCalls)
	}
}

func TestMCPM11DriveSearchRejectsRawQueryParentWithSharedDrive(t *testing.T) {
	result, handlerCalls := callMCPWaveANativeBuildArgs(t, "drive_search", map[string]any{
		"query":     "mimeType = 'application/pdf'",
		"raw_query": true,
		"parent":    "folder-123",
		"drive_id":  "drive-123",
	})
	if !result.IsError || handlerCalls != 0 {
		t.Fatalf("raw query/parent conflict result=%#v handlerCalls=%d", result.Content, handlerCalls)
	}
	text := mcpResultText(result)
	if !strings.Contains(text, "--raw-query") || !strings.Contains(text, "--parent") {
		t.Fatalf("conflict error = %q", text)
	}
}

func TestMCPM11DriveSearchReadPolicy(t *testing.T) {
	tool := findMCPTool(t, "drive_search")
	if tool.Risk != mcpRiskRead {
		t.Fatalf("risk = %q, want %q", tool.Risk, mcpRiskRead)
	}
	for _, cmd := range []McpCmd{
		{},
		{AllowTool: []string{"read"}},
		{AllowTool: []string{"drive"}},
		{AllowTool: []string{"drive.*"}},
		{AllowWrite: true, AllowTool: []string{"all"}},
	} {
		if !hasMCPTool(mcpEnabledTools(cmd), "drive_search") {
			t.Fatalf("selector %#v omitted drive_search", cmd.AllowTool)
		}
	}
}

func TestMCPM11DriveSearchSharedDriveStructuredResultThroughRunner(t *testing.T) {
	t.Setenv("GOG_MCP_M11_DRIVE_SEARCH_HELPER", "1")
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self:           os.Args[0],
		tool:           findMCPTool(t, "drive_search"),
		commandArgs:    []string{"-test.run=TestMCPM11DriveSearchRunnerHelper$"},
		timeout:        5 * time.Second,
		maxOutputBytes: 4096,
	})
	got := requireMCPNativeCommandResult(t, result)
	if result.IsError || got.ExitCode != 0 {
		t.Fatalf("drive search result = %#v", got)
	}
	if got.Tool != "drive_search" || got.Service != "drive" || got.Risk != string(mcpRiskRead) {
		t.Fatalf("runner metadata = %#v", got)
	}
	if got.Stderr != "" {
		t.Fatalf("drive search stderr = %q", got.Stderr)
	}
	stdout, ok := got.Stdout.(map[string]any)
	if !ok {
		t.Fatalf("drive search stdout type = %T, value=%#v", got.Stdout, got.Stdout)
	}
	files, ok := stdout["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("drive search files = %#v", stdout["files"])
	}
	if stdout["nextPageToken"] != "next-token" {
		t.Fatalf("drive search nextPageToken = %#v", stdout["nextPageToken"])
	}
}

func TestMCPM11DriveSearchRunnerHelper(t *testing.T) {
	if os.Getenv("GOG_MCP_M11_DRIVE_SEARCH_HELPER") != "1" {
		return
	}
	const driveID = "0AFakeSharedDriveID"
	requestSeen := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || strings.TrimPrefix(r.URL.Path, "/drive/v3") != "/files" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		for key, want := range map[string]string{
			"supportsAllDrives":         "true",
			"includeItemsFromAllDrives": "true",
			"corpora":                   "drive",
			"driveId":                   driveID,
		} {
			if got := q.Get(key); got != want {
				http.Error(w, "want "+key+"="+want+", got "+got, http.StatusBadRequest)
				return
			}
		}
		if got := q.Get("q"); got != "fullText contains 'report' and trashed = false" {
			http.Error(w, "unexpected query: "+got, http.StatusBadRequest)
			return
		}
		requestSeen = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files":         []map[string]any{{"id": "file-1", "name": "Shared report", "driveId": driveID}},
			"nextPageToken": "next-token",
		})
	}))
	defer srv.Close()

	driveSvc := newGoogleTestServiceWithEndpoint(t, srv.Client(), srv.URL+"/", drive.NewService)
	result := executeWithTestRuntime(t, []string{
		"--json", "--account", "test@example.com", "drive", "search", "--max", "2", "--drive", driveID, "--", "report",
	}, &app.Runtime{Services: app.Services{Drive: stubDriveService(driveSvc)}})
	if !requestSeen {
		result.stderr += "drive_search shared-drive fixture did not receive the expected API request\n"
	}
	mcpNativeEmitExecuteResult(result)
}

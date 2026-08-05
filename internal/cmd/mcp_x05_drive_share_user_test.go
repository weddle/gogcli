package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"google.golang.org/api/drive/v3"

	"github.com/steipete/gogcli/internal/app"
)

func TestMCPX05DriveShareUserBuildArgsExact(t *testing.T) {
	tool := mcpDriveShareUserTool()
	tests := []struct {
		name string
		args map[string]any
		want []string
	}{
		{
			name: "reader default",
			args: map[string]any{"file_id": " file-1 ", "email": " user@example.com "},
			want: []string{"drive", "share", "--to", "user", "--email", "user@example.com", "--role", "reader", "--", "file-1"},
		},
		{
			name: "commenter",
			args: map[string]any{"file_id": "file-1", "email": "user@example.com", "role": "commenter"},
			want: []string{"drive", "share", "--to", "user", "--email", "user@example.com", "--role", "commenter", "--", "file-1"},
		},
		{
			name: "writer",
			args: map[string]any{"file_id": "file-1", "email": "user@example.com", "role": "writer"},
			want: []string{"drive", "share", "--to", "user", "--email", "user@example.com", "--role", "writer", "--", "file-1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tt.args}})
			if err != nil {
				t.Fatalf("BuildArgs: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("argv = %#v, want %#v", got, tt.want)
			}
			for _, forbidden := range []string{"--to=anyone", "--domain", "--discoverable", "--notify", "--force", "--path", "--action"} {
				if slicesContains(got, forbidden) {
					t.Fatalf("argv contains forbidden %q: %#v", forbidden, got)
				}
			}
		})
	}

	for _, tt := range []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "missing file", args: map[string]any{"email": "user@example.com"}, want: "file_id"},
		{name: "missing email", args: map[string]any{"file_id": "file-1"}, want: "email"},
		{name: "empty file", args: map[string]any{"file_id": " \t", "email": "user@example.com"}, want: "empty file_id"},
		{name: "empty email", args: map[string]any{"file_id": "file-1", "email": " \t"}, want: "empty email"},
		{name: "display name email", args: map[string]any{"file_id": "file-1", "email": "User <user@example.com>"}, want: "invalid --email"},
		{name: "malformed email", args: map[string]any{"file_id": "file-1", "email": "user@@example.com"}, want: "invalid --email"},
		{name: "owner role", args: map[string]any{"file_id": "file-1", "email": "user@example.com", "role": "owner"}, want: "invalid --role"},
		{name: "unknown role", args: map[string]any{"file_id": "file-1", "email": "user@example.com", "role": "admin"}, want: "invalid --role"},
	} {
		t.Run("reject/"+tt.name, func(t *testing.T) {
			_, err := tool.BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: tt.args}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("BuildArgs error = %v, want text %q", err, tt.want)
			}
		})
	}
}

func TestMCPX05DriveShareUserSchemaAndPolicy(t *testing.T) {
	spec := mcpDriveShareUserTool()
	if spec.Risk != mcpRiskDestructive {
		t.Fatalf("risk = %q, want %q", spec.Risk, mcpRiskDestructive)
	}
	tool := newMCPTool(spec)
	if closed, ok := tool.InputSchema.AdditionalProperties.(bool); !ok || closed {
		t.Fatalf("AdditionalProperties = %#v, want false", tool.InputSchema.AdditionalProperties)
	}
	if !reflect.DeepEqual(tool.InputSchema.Required, []string{"file_id", "email"}) {
		t.Fatalf("required fields = %#v, want [file_id email]", tool.InputSchema.Required)
	}
	for _, field := range []string{"file_id", "email", "role"} {
		if _, ok := tool.InputSchema.Properties[field]; !ok {
			t.Fatalf("schema missing %q", field)
		}
	}
	for _, excluded := range []string{"to", "anyone", "domain", "discoverable", "notify", "owner", "force", "action", "path", "args", "argv", "generic"} {
		if _, ok := tool.InputSchema.Properties[excluded]; ok {
			t.Fatalf("schema exposes excluded field %q", excluded)
		}
	}

	for _, tt := range []struct {
		name        string
		allowWrite  bool
		selectors   []string
		wantVisible bool
	}{
		{name: "default", wantVisible: false},
		{name: "write only", allowWrite: true, selectors: []string{"write"}},
		{name: "drive service", allowWrite: true, selectors: []string{"drive"}},
		{name: "all", allowWrite: true, selectors: []string{"all"}},
		{name: "destructive wildcard", allowWrite: true, selectors: []string{"destructive.*"}},
		{name: "destructive", allowWrite: true, selectors: []string{"destructive"}, wantVisible: true},
		{name: "exact", allowWrite: true, selectors: []string{"drive_share_user"}, wantVisible: true},
		{name: "exact without write", selectors: []string{"drive_share_user"}},
	} {
		t.Run("policy/"+tt.name, func(t *testing.T) {
			if got := mcpToolVisible(spec, tt.allowWrite, tt.selectors); got != tt.wantVisible {
				t.Fatalf("visible = %t, want %t", got, tt.wantVisible)
			}
		})
	}

	s := newMCPServer()
	handlerCalls := 0
	s.AddTool(tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		handlerCalls++
		return mcp.NewToolResultText("handler reached"), nil
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
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "gog-x05-test", Version: "1"}
	if _, err := client.Initialize(t.Context(), initRequest); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "unknown notify", args: map[string]any{"file_id": "file-1", "email": "user@example.com", "notify": true}, want: "notify"},
		{name: "unknown generic argv", args: map[string]any{"file_id": "file-1", "email": "user@example.com", "argv": []any{"drive", "share"}}, want: "argv"},
		{name: "missing email", args: map[string]any{"file_id": "file-1"}, want: "email"},
		{name: "wrong role type", args: map[string]any{"file_id": "file-1", "email": "user@example.com", "role": true}, want: "role"},
	} {
		t.Run("schema/"+tt.name, func(t *testing.T) {
			before := handlerCalls
			result, err := client.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: spec.Name, Arguments: tt.args}})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError || handlerCalls != before || !strings.Contains(mcpResultText(result), tt.want) {
				t.Fatalf("result = %#v, calls = %d, want schema error containing %q", result.Content, handlerCalls, tt.want)
			}
		})
	}
}

func TestMCPX05DriveShareUserServerDryRun(t *testing.T) {
	t.Setenv("GOG_MCP_X05_SHARE_HELPER", "1")
	t.Setenv("GOG_MCP_X05_SHARE_MODE", "dry-run")
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self:           os.Args[0],
		tool:           mcpDriveShareUserTool(),
		commandArgs:    []string{"-test.run=TestMCPX05DriveShareUserRunnerHelper$"},
		timeout:        5 * time.Second,
		maxOutputBytes: 16 * 1024,
	})
	got := requireMCPNativeCommandResult(t, result)
	if result.IsError || got.ExitCode != 0 {
		t.Fatalf("dry-run result = %#v", got)
	}
	if got.Risk != string(mcpRiskDestructive) || got.Service != "drive" {
		t.Fatalf("dry-run metadata = %#v", got)
	}
	request := mcpNativeObject(t, got.Stdout, "dry-run stdout")
	if request["dry_run"] != true || request["op"] != "drive.share" {
		t.Fatalf("dry-run envelope = %#v", request)
	}
	planned := mcpNativeObject(t, request["request"], "dry-run request")
	if planned["fileId"] != "file-1" || planned["sendNotificationEmail"] != false {
		t.Fatalf("dry-run request = %#v", planned)
	}
	permission := mcpNativeObject(t, planned["permission"], "dry-run permission")
	if permission["type"] != "user" || permission["emailAddress"] != "user@example.com" || permission["role"] != "commenter" || permission["allowFileDiscovery"] != false {
		t.Fatalf("dry-run permission = %#v", permission)
	}
}

func TestMCPX05DriveShareUserPermissionResponseFixture(t *testing.T) {
	t.Setenv("GOG_MCP_X05_SHARE_HELPER", "1")
	t.Setenv("GOG_MCP_X05_SHARE_MODE", "permission-response")
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self:           os.Args[0],
		tool:           mcpDriveShareUserTool(),
		commandArgs:    []string{"-test.run=TestMCPX05DriveShareUserRunnerHelper$"},
		timeout:        5 * time.Second,
		maxOutputBytes: 16 * 1024,
	})
	got := requireMCPNativeCommandResult(t, result)
	if result.IsError || got.ExitCode != 0 {
		t.Fatalf("permission response result = %#v, stderr=%q", got, got.Stderr)
	}
	stdout := mcpNativeObject(t, got.Stdout, "permission response stdout")
	if stdout["link"] != "https://example.test/file-1" || stdout["permissionId"] != "perm-1" {
		t.Fatalf("permission response envelope = %#v", stdout)
	}
	permission := mcpNativeObject(t, stdout["permission"], "permission response")
	if permission["id"] != "perm-1" || permission["type"] != "user" || permission["role"] != "commenter" || permission["emailAddress"] != "user@example.com" {
		t.Fatalf("permission response = %#v", permission)
	}
}

func TestMCPX05DriveShareUserLinkLookupFailurePreservesPermissionID(t *testing.T) {
	t.Setenv("GOG_MCP_X05_SHARE_HELPER", "1")
	t.Setenv("GOG_MCP_X05_SHARE_MODE", "link-failure")
	result := mcpRunGogTool(t.Context(), mcpRunOptions{
		self:           os.Args[0],
		tool:           mcpDriveShareUserTool(),
		commandArgs:    []string{"-test.run=TestMCPX05DriveShareUserRunnerHelper$"},
		timeout:        5 * time.Second,
		maxOutputBytes: 16 * 1024,
	})
	got := requireMCPNativeCommandResult(t, result)
	if !result.IsError || got.ExitCode == 0 {
		t.Fatalf("link-failure result = %#v, want MCP error with non-zero exit", got)
	}
	if !strings.Contains(got.Stderr, "link lookup denied") {
		t.Fatalf("link-failure stderr = %q, want provider error", got.Stderr)
	}
	if strings.Contains(got.Stderr, "unexpected compensation delete") {
		t.Fatalf("link-failure stderr = %q, want no compensating delete", got.Stderr)
	}
	stdout := mcpNativeObject(t, got.Stdout, "link-failure stdout")
	if stdout["permissionId"] != "perm-1" {
		t.Fatalf("link-failure stdout = %#v, want permissionId perm-1", stdout)
	}
	permission := mcpNativeObject(t, stdout["permission"], "link-failure permission")
	if permission["id"] != "perm-1" {
		t.Fatalf("link-failure permission = %#v, want created permission ID", permission)
	}
}

func TestMCPX05DriveShareUserRunnerHelper(t *testing.T) {
	if os.Getenv("GOG_MCP_X05_SHARE_HELPER") != "1" {
		return
	}
	arguments := map[string]any{"file_id": "file-1", "email": "user@example.com", "role": "commenter"}
	commandArgs, err := mcpDriveShareUserTool().BuildArgs(mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: arguments}})
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("GOG_MCP_X05_SHARE_MODE") == "dry-run" {
		result := executeWithTestRuntime(t, append([]string{"--json", "--account", "mcp@example.com", "--dry-run"}, commandArgs...), nil)
		mcpNativeEmitExecuteResult(result)
		return
	}

	deleteSeen := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/files/file-1/permissions":
			if r.URL.Query().Get("sendNotificationEmail") != "false" || r.URL.Query().Get("supportsAllDrives") != "true" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"unexpected permission query"}}`))
				return
			}
			var permission drive.Permission
			if err := json.NewDecoder(r.Body).Decode(&permission); err != nil || permission.Type != "user" || permission.Role != "commenter" || permission.EmailAddress != "user@example.com" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"unexpected permission body"}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(&drive.Permission{Id: "perm-1", Type: "user", Role: "commenter", EmailAddress: "user@example.com"})
		case r.Method == http.MethodGet && r.URL.Path == "/files/file-1":
			if os.Getenv("GOG_MCP_X05_SHARE_MODE") == "link-failure" {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":{"message":"link lookup denied"}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "file-1", "webViewLink": "https://example.test/file-1"})
		case r.Method == http.MethodDelete && r.URL.Path == "/files/file-1/permissions/perm-1":
			deleteSeen = true
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"unexpected compensation delete"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	driveSvc := newGoogleTestServiceWithEndpoint(t, srv.Client(), srv.URL+"/", drive.NewService)
	result := executeWithTestRuntime(t, append([]string{"--json", "--account", "mcp@example.com"}, commandArgs...), &app.Runtime{Services: app.Services{Drive: stubDriveService(driveSvc)}})
	if deleteSeen {
		result.stderr += "unexpected compensation delete\n"
	}
	mcpNativeEmitExecuteResult(result)
}

func slicesContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

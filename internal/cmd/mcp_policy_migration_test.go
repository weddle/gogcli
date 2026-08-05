package cmd

import (
	"strings"
	"testing"

	"github.com/steipete/gogcli/internal/config"
)

func mcpMigrationToolFixtures() []mcpToolSpec {
	return []mcpToolSpec{
		{Name: "calendar_events", Service: "calendar", Risk: mcpRiskRead},
		{Name: "calendar_update_event", Service: "calendar", Risk: mcpRiskWrite},
		{Name: "calendar_delete_event", Service: "calendar", Risk: mcpRiskDestructive},
	}
}

func TestMCPDestructivePolicyMigrationSelectorMatrix(t *testing.T) {
	tests := []struct {
		name        string
		allowWrite  bool
		selectors   []string
		wantRead    bool
		wantWrite   bool
		wantDestroy bool
	}{
		{name: "legacy default", wantRead: true},
		{name: "legacy allow-write", allowWrite: true, wantRead: true, wantWrite: true},
		{name: "legacy empty selector", allowWrite: true, selectors: []string{}, wantRead: true, wantWrite: true},
		{name: "read selector", allowWrite: true, selectors: []string{"read"}, wantRead: true},
		{name: "exact read selector", selectors: []string{"calendar_events"}, wantRead: true},
		{name: "exact ordinary write without authorization", selectors: []string{"calendar_update_event"}},
		{name: "exact ordinary write", allowWrite: true, selectors: []string{"calendar_update_event"}, wantWrite: true},
		{name: "service without authorization", selectors: []string{"calendar"}, wantRead: true},
		{name: "service", allowWrite: true, selectors: []string{"calendar"}, wantRead: true, wantWrite: true},
		{name: "service wildcard", allowWrite: true, selectors: []string{"calendar.*"}, wantRead: true, wantWrite: true},
		{name: "write selector", allowWrite: true, selectors: []string{"write"}, wantWrite: true},
		{name: "star selector", allowWrite: true, selectors: []string{"*"}, wantRead: true, wantWrite: true},
		{name: "all selector", allowWrite: true, selectors: []string{"all"}, wantRead: true, wantWrite: true},
		{name: "destructive without authorization", selectors: []string{"destructive"}},
		{name: "destructive selector", allowWrite: true, selectors: []string{"destructive"}, wantDestroy: true},
		{name: "exact destructive selector", allowWrite: true, selectors: []string{"calendar_delete_event"}, wantDestroy: true},
		{name: "exact destructive without authorization", selectors: []string{"calendar_delete_event"}},
		{name: "destructive wildcard", allowWrite: true, selectors: []string{"destructive.*"}},
		{name: "unknown selector", allowWrite: true, selectors: []string{"future_tool"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, tool := range mcpMigrationToolFixtures() {
				want := map[mcpToolRisk]bool{
					mcpRiskRead:        tt.wantRead,
					mcpRiskWrite:       tt.wantWrite,
					mcpRiskDestructive: tt.wantDestroy,
				}[tool.Risk]
				if got := mcpToolVisible(tool, tt.allowWrite, tt.selectors); got != want {
					t.Fatalf("mcpToolVisible(%s, allowWrite=%t, selectors=%#v) = %t, want %t", tool.Risk, tt.allowWrite, tt.selectors, got, want)
				}
			}
		})
	}
}

func TestMCPDestructivePolicyMigrationLegacyRuntimeMatrix(t *testing.T) {
	tests := []struct {
		name      string
		cmd       McpCmd
		wantRead  bool
		wantWrite bool
	}{
		{name: "omitted selector", cmd: McpCmd{AllowWrite: true}, wantRead: true, wantWrite: true},
		{name: "empty selector list", cmd: McpCmd{AllowWrite: true, AllowTool: []string{}}, wantRead: true, wantWrite: true},
		{name: "empty selector values", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"", ",", "  "}}, wantRead: true, wantWrite: true},
		{name: "read selector", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"read"}}, wantRead: true},
		{name: "exact write selector", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"calendar_update_event"}}, wantWrite: true},
		{name: "service selector", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"calendar"}}, wantRead: true, wantWrite: true},
		{name: "service wildcard selector", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"calendar.*"}}, wantRead: true, wantWrite: true},
		{name: "write selector", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"write"}}, wantWrite: true},
		{name: "all selector", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"all"}}, wantRead: true, wantWrite: true},
		{name: "star selector", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"*"}}, wantRead: true, wantWrite: true},
		{name: "unknown selector", cmd: McpCmd{AllowWrite: true, AllowTool: []string{"future_tool"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := mcpEnabledTools(tt.cmd)
			if got := hasMCPTool(tools, "calendar_events"); got != tt.wantRead {
				t.Fatalf("calendar_events visible = %t, want %t; tools=%#v", got, tt.wantRead, toolNames(tools))
			}
			if got := hasMCPTool(tools, "calendar_update_event"); got != tt.wantWrite {
				t.Fatalf("calendar_update_event visible = %t, want %t; tools=%#v", got, tt.wantWrite, toolNames(tools))
			}
			if hasMCPTool(tools, "calendar_delete_event") {
				t.Fatal("legacy runtime exposed a destructive fixture")
			}
		})
	}

	readonly := mcpEnabledToolsNoPolicy(McpCmd{
		AllowWrite: true,
		AllowTool:  []string{"all"},
	}, &RootFlags{ReadOnly: true})
	if !hasMCPTool(readonly, "calendar_events") || hasMCPTool(readonly, "calendar_update_event") {
		t.Fatalf("legacy readonly matrix = %#v", toolNames(readonly))
	}
}

func TestMCPDestructivePolicyMigrationPersistentMatrix(t *testing.T) {
	tests := []struct {
		name        string
		global      config.MCPPolicy
		accounts    map[string]config.MCPPolicy
		account     string
		runtime     McpCmd
		flags       *RootFlags
		wantRead    bool
		wantWrite   bool
		wantDestroy bool
	}{
		{
			name:     "global omitted selectors defaults read",
			global:   config.MCPPolicy{},
			wantRead: true,
		},
		{
			name:      "global exact ordinary write",
			global:    config.MCPPolicy{AllowTools: []string{"calendar_update_event"}, AllowWrite: true},
			wantWrite: true,
		},
		{
			name:      "global service",
			global:    config.MCPPolicy{AllowTools: []string{"calendar"}, AllowWrite: true},
			wantRead:  true,
			wantWrite: true,
		},
		{
			name:      "global service wildcard",
			global:    config.MCPPolicy{AllowTools: []string{"calendar.*"}, AllowWrite: true},
			wantRead:  true,
			wantWrite: true,
		},
		{
			name:     "global read selector",
			global:   config.MCPPolicy{AllowTools: []string{"read"}, AllowWrite: true},
			wantRead: true,
		},
		{
			name:      "global write selector",
			global:    config.MCPPolicy{AllowTools: []string{"write"}, AllowWrite: true},
			wantWrite: true,
		},
		{
			name:      "global star selector",
			global:    config.MCPPolicy{AllowTools: []string{"*"}, AllowWrite: true},
			wantRead:  true,
			wantWrite: true,
		},
		{
			name:      "global all selector",
			global:    config.MCPPolicy{AllowTools: []string{"all"}, AllowWrite: true},
			wantRead:  true,
			wantWrite: true,
		},
		{
			name:   "global destructive selector without write authorization",
			global: config.MCPPolicy{AllowTools: []string{"destructive"}},
		},
		{
			name:        "global destructive selector",
			global:      config.MCPPolicy{AllowTools: []string{"destructive"}, AllowWrite: true},
			wantDestroy: true,
		},
		{
			name:     "per-account replacement narrows global",
			global:   config.MCPPolicy{AllowTools: []string{"all"}, AllowWrite: true},
			accounts: map[string]config.MCPPolicy{" Personal@Example.com ": {AllowTools: []string{"read"}}},
			account:  "personal@example.com",
			wantRead: true,
		},
		{
			name:        "per-account replacement selects destructive",
			global:      config.MCPPolicy{AllowTools: []string{"all"}, AllowWrite: true},
			accounts:    map[string]config.MCPPolicy{" Personal@Example.com ": {AllowTools: []string{"destructive"}, AllowWrite: true}},
			account:     "personal@example.com",
			wantDestroy: true,
		},
		{
			name:     "runtime exact narrowing",
			global:   config.MCPPolicy{AllowTools: []string{"all"}, AllowWrite: true},
			runtime:  McpCmd{AllowTool: []string{"calendar_events"}},
			wantRead: true,
		},
		{
			name:      "runtime ordinary-class narrowing",
			global:    config.MCPPolicy{AllowTools: []string{"all"}, AllowWrite: true},
			runtime:   McpCmd{AllowTool: []string{"write"}},
			wantWrite: true,
		},
		{
			name:      "runtime empty values do not narrow",
			global:    config.MCPPolicy{AllowTools: []string{"calendar.*"}, AllowWrite: true},
			runtime:   McpCmd{AllowTool: []string{"", ",", "  "}},
			wantRead:  true,
			wantWrite: true,
		},
		{
			name:    "runtime unknown fails closed to no tools",
			global:  config.MCPPolicy{AllowTools: []string{"all"}, AllowWrite: true},
			runtime: McpCmd{AllowTool: []string{"future_tool"}},
		},
		{
			name:     "readonly suppresses configured ordinary writes",
			global:   config.MCPPolicy{AllowTools: []string{"all"}, AllowWrite: true},
			flags:    &RootFlags{ReadOnly: true},
			wantRead: true,
		},
		{
			name:   "readonly suppresses configured destructive tools",
			global: config.MCPPolicy{AllowTools: []string{"destructive"}, AllowWrite: true},
			flags:  &RootFlags{ReadOnly: true},
		},
		{
			name:        "runtime destructive narrowing preserves explicit gate",
			global:      config.MCPPolicy{AllowTools: []string{"destructive"}, AllowWrite: true},
			runtime:     McpCmd{AllowTool: []string{"destructive"}},
			wantDestroy: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.MCPConfig{MCPPolicy: tt.global, Accounts: tt.accounts}
			policy, err := selectMCPPolicy(cfg, tt.account)
			if err != nil {
				t.Fatalf("selectMCPPolicy: %v", err)
			}
			tools, err := mcpEnabledToolsWithPolicy(tt.runtime, tt.flags, policy)
			if err != nil {
				t.Fatalf("mcpEnabledToolsWithPolicy: %v", err)
			}
			if got := hasMCPTool(tools, "calendar_events"); got != tt.wantRead {
				t.Fatalf("calendar_events visible = %t, want %t; tools=%#v policy=%#v", got, tt.wantRead, toolNames(tools), policy)
			}
			if got := hasMCPTool(tools, "calendar_update_event"); got != tt.wantWrite {
				t.Fatalf("calendar_update_event visible = %t, want %t; tools=%#v policy=%#v", got, tt.wantWrite, toolNames(tools), policy)
			}
			if got := mcpMigrationDestructiveVisible(policy, tt.runtime, tt.flags); got != tt.wantDestroy {
				t.Fatalf("destructive fixture visible = %t, want %t; policy=%#v runtime=%#v flags=%#v", got, tt.wantDestroy, policy, tt.runtime, tt.flags)
			}
		})
	}

	_, err := mcpEnabledToolsWithPolicy(
		McpCmd{AllowWrite: true},
		&RootFlags{},
		config.MCPPolicy{AllowTools: []string{"read"}},
	)
	if err == nil || !strings.Contains(err.Error(), "cannot widen") {
		t.Fatalf("runtime allow-write widening error = %v", err)
	}
}

func mcpMigrationDestructiveVisible(policy config.MCPPolicy, runtime McpCmd, flags *RootFlags) bool {
	tool := mcpMigrationToolFixtures()[2]
	allowWrite := policy.AllowWrite
	if flags != nil && flags.ReadOnly {
		allowWrite = false
	}
	selectors := splitCommaValues(runtime.AllowTool)
	if len(selectors) > 0 && !mcpToolAllowed(tool, selectors) {
		return false
	}
	return mcpToolVisible(tool, allowWrite, policy.AllowTools)
}

func TestMCPDestructivePolicyMigrationValidationMatrix(t *testing.T) {
	tests := []struct {
		name          string
		policy        config.MCPPolicy
		wantErr       bool
		wantSelectors []string
	}{
		{name: "omitted selectors default to read", policy: config.MCPPolicy{}, wantSelectors: []string{"read"}},
		{name: "explicit read", policy: config.MCPPolicy{AllowTools: []string{"read"}}, wantSelectors: []string{"read"}},
		{name: "exact read", policy: config.MCPPolicy{AllowTools: []string{"calendar_events"}}, wantSelectors: []string{"calendar_events"}},
		{name: "exact ordinary write", policy: config.MCPPolicy{AllowTools: []string{"calendar_update_event"}, AllowWrite: true}, wantSelectors: []string{"calendar_update_event"}},
		{name: "service", policy: config.MCPPolicy{AllowTools: []string{"calendar"}, AllowWrite: true}, wantSelectors: []string{"calendar"}},
		{name: "service wildcard", policy: config.MCPPolicy{AllowTools: []string{"calendar.*"}, AllowWrite: true}, wantSelectors: []string{"calendar.*"}},
		{name: "write", policy: config.MCPPolicy{AllowTools: []string{"write"}, AllowWrite: true}, wantSelectors: []string{"write"}},
		{name: "star", policy: config.MCPPolicy{AllowTools: []string{"*"}, AllowWrite: true}, wantSelectors: []string{"*"}},
		{name: "all", policy: config.MCPPolicy{AllowTools: []string{"all"}, AllowWrite: true}, wantSelectors: []string{"all"}},
		{name: "destructive with write authorization", policy: config.MCPPolicy{AllowTools: []string{"destructive"}, AllowWrite: true}, wantSelectors: []string{"destructive"}},
		{name: "destructive without write authorization remains valid but hidden", policy: config.MCPPolicy{AllowTools: []string{"destructive"}, AllowWrite: false}, wantSelectors: []string{"destructive"}},
		{name: "empty list", policy: config.MCPPolicy{AllowTools: []string{}}, wantErr: true},
		{name: "empty selector value", policy: config.MCPPolicy{AllowTools: []string{"", "  "}}, wantErr: true},
		{name: "unknown selector", policy: config.MCPPolicy{AllowTools: []string{"future_tool"}}, wantErr: true},
		{name: "unknown destructive wildcard", policy: config.MCPPolicy{AllowTools: []string{"destructive.*"}, AllowWrite: true}, wantErr: true},
		{name: "future exact destructive selector before registration", policy: config.MCPPolicy{AllowTools: []string{"calendar_delete_event"}, AllowWrite: true}, wantErr: true},
		{name: "write authorization without selector", policy: config.MCPPolicy{AllowWrite: true}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := normalizeMCPPolicy(tt.policy)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeMCPPolicy(%#v) unexpectedly succeeded: %#v", tt.policy, policy)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeMCPPolicy(%#v): %v", tt.policy, err)
			}
			if strings.Join(policy.AllowTools, "\x00") != strings.Join(tt.wantSelectors, "\x00") {
				t.Fatalf("normalized selectors = %#v, want %#v", policy.AllowTools, tt.wantSelectors)
			}
		})
	}

	_, err := selectMCPPolicy(config.MCPConfig{
		MCPPolicy: config.MCPPolicy{AllowTools: []string{"read"}},
		Accounts: map[string]config.MCPPolicy{
			"unselected@example.com": {AllowTools: []string{"future_tool"}},
		},
	}, "")
	if err == nil || !strings.Contains(err.Error(), "unselected@example.com") {
		t.Fatalf("unselected account validation error = %v", err)
	}
}

//go:build !safety_profile

package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steipete/gogcli/internal/safetyprofile"
)

func withBakedSafetyProfile(t *testing.T, raw string) {
	t.Helper()
	profile, err := safetyprofile.Parse(raw)
	if err != nil {
		t.Fatalf("safetyprofile.Parse: %v", err)
	}
	allow := make(map[string]bool, len(profile.AllowRules))
	for _, r := range profile.AllowRules {
		allow[r] = true
	}
	deny := make(map[string]bool, len(profile.DenyRules))
	for _, r := range profile.DenyRules {
		deny[r] = true
	}
	prev := bakedSafetyTestProfile
	bakedSafetyTestProfile.enabled = true
	bakedSafetyTestProfile.name = profile.Name
	bakedSafetyTestProfile.allowAll = profile.AllowAll
	bakedSafetyTestProfile.hasAllowRules = profile.AllowAll || len(profile.AllowRules) > 0
	bakedSafetyTestProfile.allow = allow
	bakedSafetyTestProfile.deny = deny
	t.Cleanup(func() { bakedSafetyTestProfile = prev })
}

func TestSafetyProfileHashAgreement(t *testing.T) {
	cases := []struct {
		path []string
		rule string
	}{
		{[]string{"version"}, "version"},
		{[]string{"gmail", "send"}, "gmail.send"},
		{[]string{"gmail", "drafts", "send"}, "gmail.drafts.send"},
		{[]string{"gmail", "drafts", "create"}, "gmail.drafts.create"},
		{[]string{"calendar", "alias", "set"}, "calendar.alias.set"},
		{[]string{"a"}, "a"},
	}
	for _, c := range cases {
		runtime := bakedSafetyHashPath(c.path)
		codegen := safetyprofile.HashRule(c.rule)
		if runtime != codegen {
			t.Fatalf("hash mismatch for %v / %q: runtime=%#x codegen=%#x", c.path, c.rule, runtime, codegen)
		}
	}
}

func TestBakedSafetyProfileBlocksBeforeRuntimeAllowlist(t *testing.T) {
	setTestConfigHome(t)
	withBakedSafetyProfile(t, `
name: test
allow:
  - version
deny:
  - gmail.send
  - send
`)

	err := Execute([]string{"--enable-commands", "gmail.send", "gmail", "send", "--to", "a@example.com", "--subject", "S", "--body", "B"})
	if err == nil {
		t.Fatalf("expected baked safety profile block")
	}
	if got := err.Error(); !strings.Contains(got, "baked safety profile") || !strings.Contains(got, "gmail send") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBakedSafetyProfileFailsClosed(t *testing.T) {
	setTestConfigHome(t)
	withBakedSafetyProfile(t, `
name: readonly
allow:
  - version
`)

	err := Execute([]string{"tasks", "list", "task-list-1"})
	if err == nil {
		t.Fatalf("expected fail-closed safety profile block")
	}
	if got := err.Error(); !strings.Contains(got, "not included") || !strings.Contains(got, "tasks list") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBakedSafetyProfileAllowsListedCommand(t *testing.T) {
	setTestConfigHome(t)
	withBakedSafetyProfile(t, `
name: test
allow:
  - version
`)

	if err := Execute([]string{"version"}); err != nil {
		t.Fatalf("expected allowed command, got %v", err)
	}
}

func TestReadonlySafetyProfileBlocksNestedMutations(t *testing.T) {
	setTestConfigHome(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "safety-profiles", "readonly.yaml"))
	if err != nil {
		t.Fatalf("read readonly profile: %v", err)
	}
	withBakedSafetyProfile(t, string(raw))

	tests := [][]string{
		{"gmail", "messages", "modify", "msg-1", "--add", "Label_1"},
		{"calendar", "alias", "set", "work", "abc123@group.calendar.google.com"},
		{"calendar", "alias", "unset", "work"},
	}
	for _, args := range tests {
		err := Execute(args)
		if err == nil {
			t.Fatalf("expected readonly profile block for %v", args)
		}
		if got := err.Error(); !strings.Contains(got, "baked safety profile") {
			t.Fatalf("unexpected error for %v: %v", args, err)
		}
	}
}

func TestBundledSafetyProfilesExposeAutomationSchema(t *testing.T) {
	setTestConfigHome(t)

	for _, profile := range []string{"agent-safe.yaml", "readonly.yaml"} {
		profile := profile
		t.Run(profile, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "safety-profiles", profile))
			if err != nil {
				t.Fatalf("read %s: %v", profile, err)
			}
			withBakedSafetyProfile(t, string(raw))

			out := captureStdout(t, func() {
				_ = captureStderr(t, func() {
					if err := Execute([]string{"schema"}); err != nil {
						t.Fatalf("Execute(schema): %v", err)
					}
				})
			})
			var doc schemaDoc
			if err := json.Unmarshal([]byte(out), &doc); err != nil {
				t.Fatalf("unmarshal schema: %v", err)
			}
			if !doc.Automation.Safety.BakedProfile.Enabled {
				t.Fatalf("expected baked profile metadata: %#v", doc.Automation.Safety.BakedProfile)
			}
			if doc.Automation.Safety.BakedProfile.Name != strings.TrimSuffix(profile, ".yaml") {
				t.Fatalf("profile name = %q", doc.Automation.Safety.BakedProfile.Name)
			}
		})
	}
}

func TestBundledSafetyProfilesAllowDocsSuggestions(t *testing.T) {
	setTestConfigHome(t)

	for _, profile := range []string{"agent-safe.yaml", "readonly.yaml"} {
		profile := profile
		t.Run(profile, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "safety-profiles", profile))
			if err != nil {
				t.Fatalf("read %s: %v", profile, err)
			}
			withBakedSafetyProfile(t, string(raw))

			out := captureStdout(t, func() {
				_ = captureStderr(t, func() {
					if err := Execute([]string{"docs", "suggestions", "list", "--help"}); err != nil {
						t.Fatalf("Execute: %v", err)
					}
				})
			})
			if !strings.Contains(out, "List pending text insertions and deletions") {
				t.Fatalf("expected docs suggestions help in %s profile, got: %q", profile, out)
			}
			if strings.Contains(out, "blocked by baked safety profile") {
				t.Fatalf("expected docs suggestions to be allowed by %s profile, got: %q", profile, out)
			}
		})
	}
}

func TestReadonlySafetyProfileFiltersHelp(t *testing.T) {
	setTestConfigHome(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "safety-profiles", "readonly.yaml"))
	if err != nil {
		t.Fatalf("read readonly profile: %v", err)
	}
	withBakedSafetyProfile(t, string(raw))

	out := captureStdout(t, func() {
		_ = captureStderr(t, func() {
			if err := Execute([]string{"gmail", "messages", "--help"}); err != nil {
				t.Fatalf("Execute: %v", err)
			}
		})
	})
	if !strings.Contains(out, "\n  search") {
		t.Fatalf("expected search in filtered help, got: %q", out)
	}
	if strings.Contains(out, "\n  modify") {
		t.Fatalf("expected modify to be hidden from readonly help, got: %q", out)
	}
	if strings.Contains(out, "\nOrganize\n") {
		t.Fatalf("expected empty command group to be hidden from readonly help, got: %q", out)
	}

	out = captureStdout(t, func() {
		_ = captureStderr(t, func() {
			if err := Execute([]string{"calendar", "alias", "--help"}); err != nil {
				t.Fatalf("Execute: %v", err)
			}
		})
	})
	if !strings.Contains(out, "\n  list") {
		t.Fatalf("expected list in filtered help, got: %q", out)
	}
	if strings.Contains(out, "\n  set ") || strings.Contains(out, "\n  unset ") {
		t.Fatalf("expected alias writes to be hidden from readonly help, got: %q", out)
	}
}

func TestAgentSafeProfileFiltersHelp(t *testing.T) {
	setTestConfigHome(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "safety-profiles", "agent-safe.yaml"))
	if err != nil {
		t.Fatalf("read agent-safe profile: %v", err)
	}
	withBakedSafetyProfile(t, string(raw))

	out := captureStdout(t, func() {
		_ = captureStderr(t, func() {
			if err := Execute([]string{"gmail", "drafts", "--help"}); err != nil {
				t.Fatalf("Execute: %v", err)
			}
		})
	})
	if !strings.Contains(out, "\n  create") {
		t.Fatalf("expected create in filtered help, got: %q", out)
	}
	if strings.Contains(out, "\n  send ") {
		t.Fatalf("expected send to be hidden from agent-safe help, got: %q", out)
	}

	blocked := captureStdout(t, func() {
		_ = captureStderr(t, func() {
			if err := Execute([]string{"gmail", "drafts", "send", "--help"}); err != nil {
				t.Fatalf("Execute: %v", err)
			}
		})
	})
	if !strings.Contains(blocked, `command "gmail drafts send" is blocked by baked safety profile "agent-safe"`) {
		t.Fatalf("expected blocked help message, got: %q", blocked)
	}
	if strings.Contains(blocked, "Send a draft") {
		t.Fatalf("expected blocked command docs to be hidden, got: %q", blocked)
	}
}

func TestSafetyProfileFiltersSchema(t *testing.T) {
	setTestConfigHome(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "safety-profiles", "agent-safe.yaml"))
	if err != nil {
		t.Fatalf("read agent-safe profile: %v", err)
	}
	withBakedSafetyProfile(t, string(raw))

	out := captureStdout(t, func() {
		_ = captureStderr(t, func() {
			if err := Execute([]string{"schema", "gmail drafts"}); err != nil {
				t.Fatalf("Execute: %v", err)
			}
		})
	})
	if !strings.Contains(out, `"name": "create"`) {
		t.Fatalf("expected create in filtered schema, got: %q", out)
	}
	if strings.Contains(out, `"name": "send"`) {
		t.Fatalf("expected send to be hidden from filtered schema, got: %q", out)
	}
}

func TestBakedCalendarSafetyProfileOutcomes(t *testing.T) {
	profiles := []struct {
		name            string
		file            string
		blockedCommands [][]string
		allowedCommands [][]string
		unmanaged       [][]string
	}{
		{
			name: "agent-safe",
			file: "agent-safe.yaml",
			blockedCommands: [][]string{
				{"calendar", "subscribe", "raw@example.com"},
				{"calendar", "out-of-office", "--from", "2026-08-04T09:00:00Z", "--to", "2026-08-04T10:00:00Z"},
				{"calendar", "working-location", "--from", "2026-08-04", "--to", "2026-08-05", "--type", "home"},
			},
			allowedCommands: [][]string{
				{"calendar", "focus-time"},
				{"calendar", "create-calendar"},
			},
			unmanaged: [][]string{
				{"calendar", "unsubscribe", "raw@example.com"},
			},
		},
		{
			name: "readonly",
			file: "readonly.yaml",
			blockedCommands: [][]string{
				{"calendar", "create", "--summary", "Plan", "--from", "2026-08-04T09:00:00Z", "--to", "2026-08-04T10:00:00Z", "primary"},
				{"calendar", "respond", "--status", "accepted", "primary", "event-1"},
				{"calendar", "move", "primary", "event-1", "destination@example.com"},
				{"calendar", "create-calendar", "Team"},
				{"calendar", "subscribe", "raw@example.com"},
				{"calendar", "focus-time", "--from", "2026-08-04T09:00:00Z", "--to", "2026-08-04T10:00:00Z"},
				{"calendar", "out-of-office", "--from", "2026-08-04T09:00:00Z", "--to", "2026-08-04T10:00:00Z"},
				{"calendar", "working-location", "--from", "2026-08-04", "--to", "2026-08-05", "--type", "home"},
			},
			unmanaged: [][]string{
				{"calendar", "unsubscribe", "raw@example.com"},
			},
		},
	}

	for _, profile := range profiles {
		profile := profile
		t.Run(profile.name, func(t *testing.T) {
			setTestConfigHome(t)
			raw, err := os.ReadFile(filepath.Join("..", "..", "safety-profiles", profile.file))
			if err != nil {
				t.Fatalf("read %s: %v", profile.file, err)
			}
			withBakedSafetyProfile(t, string(raw))

			runHelp := func(args ...string) (string, string, error) {
				var stdout, stderr string
				var runErr error
				stdout = captureStdout(t, func() {
					stderr = captureStderr(t, func() {
						runErr = Execute(append(append([]string{}, args...), "--help"))
					})
				})
				return stdout, stderr, runErr
			}

			runCommand := func(args ...string) (string, string, error) {
				var stdout, stderr string
				var runErr error
				stdout = captureStdout(t, func() {
					stderr = captureStderr(t, func() {
						runErr = Execute(args)
					})
				})
				return stdout, stderr, runErr
			}

			for _, args := range profile.blockedCommands {
				args := args
				t.Run("blocked/"+strings.Join(args, "-"), func(t *testing.T) {
					stdout, stderr, runErr := runCommand(args...)
					if runErr == nil {
						t.Fatalf("Execute(%v) unexpectedly succeeded; stdout=%q stderr=%q", args, stdout, stderr)
					}
					if got := ExitCode(runErr); got != 2 {
						t.Fatalf("Execute(%v) exit code = %d, want 2; err=%v stderr=%q", args, got, runErr, stderr)
					}
					if !strings.Contains(runErr.Error(), "baked safety profile") || !strings.Contains(stderr, "baked safety profile") {
						t.Fatalf("Execute(%v) error=%v stderr=%q, want baked-profile refusal", args, runErr, stderr)
					}
					if stdout != "" {
						t.Fatalf("blocked Execute(%v) leaked help to stdout: %q", args, stdout)
					}
				})
			}

			for _, args := range profile.allowedCommands {
				args := args
				t.Run("allowed/"+strings.Join(args, "-"), func(t *testing.T) {
					stdout, stderr, runErr := runHelp(args...)
					if runErr != nil {
						t.Fatalf("Execute(%v) = %v; stdout=%q stderr=%q", args, runErr, stdout, stderr)
					}
					if !strings.Contains(stdout, "Usage:") {
						t.Fatalf("allowed Execute(%v) did not print help: %q", args, stdout)
					}
					if strings.Contains(stdout, "blocked by baked safety profile") || strings.Contains(stderr, "baked safety profile") {
						t.Fatalf("allowed Execute(%v) reported baked-profile refusal: stdout=%q stderr=%q", args, stdout, stderr)
					}
				})
			}

			for _, args := range profile.unmanaged {
				args := args
				t.Run("unmanaged/"+strings.Join(args, "-"), func(t *testing.T) {
					stdout, stderr, runErr := runCommand(args...)
					if runErr == nil {
						t.Fatalf("Execute(%v) unexpectedly allowed unmanaged command; stdout=%q stderr=%q", args, stdout, stderr)
					}
					if got := ExitCode(runErr); got != 2 {
						t.Fatalf("Execute(%v) exit code = %d, want 2; err=%v stderr=%q", args, got, runErr, stderr)
					}
					if !strings.Contains(runErr.Error(), "not included in baked safety profile") ||
						!strings.Contains(stderr, "not included in baked safety profile") {
						t.Fatalf("Execute(%v) error=%v stderr=%q, want unmanaged fail-closed refusal", args, runErr, stderr)
					}
					if stdout != "" {
						t.Fatalf("unmanaged Execute(%v) leaked help to stdout: %q", args, stdout)
					}
				})
			}
		})
	}
}

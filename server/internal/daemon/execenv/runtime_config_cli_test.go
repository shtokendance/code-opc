package execenv

import (
	"strings"
	"testing"
)

func TestRuntimeBriefPinsTaskCLI(t *testing.T) {
	for kind, ctx := range map[string]TaskContextForEnv{
		"issue": {IssueID: "issue-1"}, "chat": {ChatSessionID: "chat-1"},
		"autopilot": {AutopilotRunID: "run-1"}, "quick-create": {QuickCreatePrompt: "create work"},
	} {
		brief := buildMetaSkillContent("codex", ctx)
		want := "Use the `multica` CLI to interact with the platform."
		if !strings.Contains(brief, want) {
			t.Errorf("%s brief missing %q", kind, want)
		}
		if kind == "quick-create" && !strings.Contains(brief, "Run exactly one `multica issue create --output json` invocation") {
			t.Errorf("quick-create workflow does not use multica CLI")
		}
	}
}

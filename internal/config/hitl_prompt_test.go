package config

import (
	"strings"
	"testing"
)

func TestDefaultHitlAuditAgentPromptIncludesPrioritizedRules(t *testing.T) {
	prompt := DefaultHitlAuditAgentPrompt()
	for _, want := range []string{
		"If both reject and approve rules are matched, you must reject",
		"Modifying/resetting any user or administrator password",
		"Modifying/creating/deleting users, roles, or permissions",
		"Stopping, disabling, or restarting business services",
		"matched rule: ...",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default approval prompt missing %q", want)
		}
	}
}

func TestDefaultHitlAuditAgentPromptReviewEditKeepsEditedArguments(t *testing.T) {
	prompt := DefaultHitlAuditAgentPromptReviewEdit()
	if !strings.Contains(prompt, `"editedArguments":{...}`) {
		t.Fatal("review-edit prompt must preserve editedArguments output")
	}
	if !strings.Contains(prompt, "matched rule: ...") {
		t.Fatal("review-edit prompt must require a matched rule")
	}
}

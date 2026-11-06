package agent_test

import (
	"testing"
	"unicode"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/projectprompt"
)

func TestBuiltInOperationalPromptsAreEnglish(t *testing.T) {
	prompts := map[string]string{
		"single agent":           agent.DefaultSingleAgentSystemPrompt(),
		"audit":                  config.DefaultHitlAuditAgentPrompt(),
		"audit argument editing": config.DefaultHitlAuditAgentPromptReviewEdit(),
		"coordinator facts":      projectprompt.FactRecordingBlackboardSection(true),
		"subagent facts":         projectprompt.FactRecordingSubAgentSection(),
		"project continuation":   projectprompt.ProjectResumeProtocol(),
	}
	for name, prompt := range prompts {
		for _, r := range prompt {
			if unicode.Is(unicode.Han, r) {
				t.Errorf("%s contains untranslated text", name)
				break
			}
		}
	}
}

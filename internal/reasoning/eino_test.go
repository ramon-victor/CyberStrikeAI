package reasoning

import (
	"testing"

	"cyberstrike-ai/internal/config"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
)

func TestEffortStringForAPI_passthrough(t *testing.T) {
	cases := map[string]string{
		"max":    "max",
		"xhigh":  "xhigh",
		"HIGH":   "high",
		"Medium": "medium",
	}
	for in, want := range cases {
		if got := effortStringForAPI(in); got != want {
			t.Fatalf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeEffort_maxAndXhigh(t *testing.T) {
	if normalizeEffort("xhigh") != "xhigh" {
		t.Fatal("xhigh not accepted")
	}
	if normalizeEffort("max") != "max" {
		t.Fatal("max not accepted")
	}
}

func TestApplyOpenAICompat_xhighExtraField(t *testing.T) {
	cfg := &einoopenai.ChatModelConfig{}
	oa := &config.OpenAIConfig{
		Reasoning: config.OpenAIReasoningConfig{
			Profile: "openai_compat",
			Mode:    "on",
			Effort:  "xhigh",
		},
	}
	ApplyToEinoChatModelConfig(cfg, oa, nil)
	if cfg.ExtraFields == nil {
		t.Fatal("expected ExtraFields")
	}
	if got, _ := cfg.ExtraFields["reasoning_effort"].(string); got != "xhigh" {
		t.Fatalf("reasoning_effort=%q", got)
	}
}

func TestApplyOpenAICompat_maxPassthrough(t *testing.T) {
	cfg := &einoopenai.ChatModelConfig{}
	oa := &config.OpenAIConfig{
		Reasoning: config.OpenAIReasoningConfig{
			Profile: "openai_compat",
			Mode:    "on",
			Effort:  "max",
		},
	}
	ApplyToEinoChatModelConfig(cfg, oa, nil)
	got, _ := cfg.ExtraFields["reasoning_effort"].(string)
	if got != "max" {
		t.Fatalf("max effort wire=%q, want max", got)
	}
}

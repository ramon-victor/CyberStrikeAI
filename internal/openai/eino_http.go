package openai

import (
	"net/http"

	"cyberstrike-ai/internal/config"
)

// NewEinoHTTPClient adds OpenAI-compatible request fixes, SSE error decoding,
// and successful-stream heartbeat sanitation.
// Claude channels use Eino's native agenticclaude model and never enter here.
func NewEinoHTTPClient(cfg *config.OpenAIConfig, base *http.Client) *http.Client {
	return newEinoHTTPClient(cfg, base, false)
}

// NewEinoAgenticHTTPClient also coalesces parallel tool deltas before the native
// Eino converter, which cannot associate interleaved fragments by source index.
func NewEinoAgenticHTTPClient(cfg *config.OpenAIConfig, base *http.Client) *http.Client {
	return newEinoHTTPClient(cfg, base, true)
}

func newEinoHTTPClient(cfg *config.OpenAIConfig, base *http.Client, native bool) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	cloned := *base
	transport := base.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	transport = &reasoningToolChoiceCompatRoundTripper{base: transport, cfg: cfg}
	transport = &einoSSEErrorRoundTripper{base: transport}
	transport = &einoSSESanitizingRoundTripper{base: transport, coalesceToolCalls: native}
	transport = &einoUsageNormalizingRoundTripper{base: transport}
	transport = &headersRoundTripper{base: transport, cfg: cfg}
	cloned.Transport = transport
	return customHeaderHTTPClient(&cloned, cfg)
}

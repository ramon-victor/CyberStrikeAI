package project

import (
	"fmt"
	"strings"
)

// Fact category constants written to the category field of upsert_project_fact.
const (
	FactCategoryTarget   = "target"
	FactCategoryAuth     = "auth"
	FactCategoryInfra    = "infra"
	FactCategoryBusiness = "business"
	FactCategoryFinding  = "finding"
	FactCategoryChain    = "chain"
	FactCategoryExploit  = "exploit"
	FactCategoryPOC      = "poc"
	FactCategoryNote     = "note"
)

// RequiresAttackChainBody reports whether a fact should carry reproducible attack-chain / exploit details in body, not only in summary.
func RequiresAttackChainBody(category, factKey string) bool {
	c := strings.ToLower(strings.TrimSpace(category))
	switch c {
	case FactCategoryFinding, FactCategoryChain, FactCategoryExploit, FactCategoryPOC, "vuln":
		return true
	}
	key := strings.ToLower(strings.TrimSpace(factKey))
	for _, prefix := range []string{"finding/", "chain/", "exploit/", "poc/"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// IsSparseFactBody returns true when an attack-chain fact body is too short or lacks key sections; this is soft validation and does not block writes.
func IsSparseFactBody(category, factKey, body string) bool {
	if !RequiresAttackChainBody(category, factKey) {
		return false
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return true
	}
	lower := strings.ToLower(body)
	// Require at least one reproducibility clue: steps, request, command, or code block.
	hasSteps := strings.Contains(lower, "attack chain") || strings.Contains(lower, "## attack") ||
		strings.Contains(lower, "## exploit") || strings.Contains(lower, "## poc")
	hasHTTP := strings.Contains(lower, "```http") || strings.Contains(lower, "```bash") ||
		strings.Contains(lower, "curl ") || strings.Contains(lower, "get ") || strings.Contains(lower, "post ")
	hasReq := strings.Contains(lower, "request") || strings.Contains(lower, "response") || strings.Contains(lower, "payload")
	// Without attack-chain, POC, request, or similar structural clues, treat it as conclusion-only regardless of length.
	return !(hasSteps || hasHTTP || hasReq)
}

// FactBodyTemplate returns the recommended body Markdown skeleton for a category, for an Agent to fill with real content.
func FactBodyTemplate(category, factKey string) string {
	if RequiresAttackChainBody(category, factKey) {
		return attackChainFactBodyTemplate
	}
	return envFactBodyTemplate
}

const attackChainFactBodyTemplate = `## Conclusion (verifiable, one sentence)
<Do not only write "vulnerability exists"; state the type, location, and trigger condition>

## Target and entry point
- Target: <URL / IP:Port / hostname>
- Entry point: <path / endpoint / parameter>
- Preconditions: <anonymous / role / Cookie / other dependency>

## Attack chain (step-by-step reproducible)
1. <reconnaissance/discovery>
2. <exploitation/trigger>
3. <impact proof, such as file read, RCE echo, unauthorized data access>

## Exploit / POC
### Request
` + "```http\n<METHOD> <path> HTTP/1.1\nHost: ...\n...\n\n<body>\n```" + `

### Response / observed behavior
<key response fragment, status code, difference>

### Command / script (if any)
` + "```bash\n<command>\n```" + `

## Key evidence
- <tool output summary / screenshot path / session or message ID>

## Relationships
- related_vulnerability_id: <optional, corresponding record_vulnerability id>
- dependent facts: <fact_key, such as auth/session_cookie>

## Notes and uncertainty
<pending hypotheses, environment differences, bypass attempts recorded>`

const envFactBodyTemplate = `## Summary
<core understanding captured by this fact>

## Details
<ports / versions / paths / credential characteristics / business rules>

## Sources and evidence
<command output, response fragment, discovery time>

## Relationships
- related fact_key: <optional>`

// FactRecordingGuidanceBlock writes system-prompt guidance requiring facts to preserve attack-chain context instead of conclusions only.
func FactRecordingGuidanceBlock() string {
	return `### Fact writing rules (audit reproduction / knowledge retention)

- **summary**: one-line index entry; it must include what, where, and how to trigger or verify. Do not write only a conclusion, such as only "SQLi exists".
- **body**: complete reproducible context written to the body field of ` + "`upsert_project_fact`" + `; the index does not contain body, so later sessions must retrieve it with ` + "`get_project_fact`" + `.
- **category / fact_key recommendations**:
  - Environment knowledge: ` + "`target/`" + `, ` + "`auth/`" + `, ` + "`infra/`" + `, ` + "`business/`" + `; the environment template is enough for body.
  - Findings and exploitation: ` + "`finding/`" + `, ` + "`chain/`" + `, ` + "`exploit/`" + `, ` + "`poc/`" + `; **must** fill body with the attack-chain template: entry point, step-by-step attack chain, raw requests/responses or commands, evidence, and related vulnerability ID.
- **Division from vulnerability records**: ` + "`record_vulnerability`" + ` stores deliverable findings; facts store **all context needed for reproduction**, including failed attempts, bypasses, and dependent sessions. A finding may be recorded in both places.
- When updating the same finding, keep the same ` + "`fact_key`" + ` and overwrite it; do not scatter context across multiple keys.`
}

// SparseBodyWarning is the tool response hint returned when an attack-chain fact body is insufficient; it does not block saving.
func SparseBodyWarning(category, factKey string) string {
	if !IsSparseFactBody(category, factKey, "") {
		return ""
	}
	return fmt.Sprintf(
		"\n\nWarning: category=%q / fact_key=%q is an attack-chain fact, but body is empty or too sparse. Add the complete attack chain and POC, using the template, so later audits can reproduce it.\nSuggested body skeleton:\n%s",
		category, factKey, FactBodyTemplate(category, factKey),
	)
}

// SparseBodyWarningIfNeeded appends a warning based on the actual body.
func SparseBodyWarningIfNeeded(category, factKey, body string) string {
	if !IsSparseFactBody(category, factKey, body) {
		return ""
	}
	return SparseBodyWarning(category, factKey)
}

package project

import (
	"fmt"
	"strings"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
)

// AppendSystemPromptBlock 将附加块追加到 system prompt。
func AppendSystemPromptBlock(base, block string) string {
	base = strings.TrimSpace(base)
	block = strings.TrimSpace(block)
	if block == "" {
		return base
	}
	if base == "" {
		return block
	}
	return base + "\n\n" + block
}

const (
	factIndexFooterGetDetail = "For complete content (attack chains, POCs, requests and responses, and so on), you must call get_project_fact(fact_key). Never invent details from a summary."
	factIndexFooterWriteHint = "When writing fact links, use from (source fact_key -> current fact), for example {from:target/*, type:discovered_on} on a finding. The body must contain the complete reproducible flow; finding, chain, exploit, and POC fact keys should use the finding|chain|exploit|poc/ prefixes."
	factIndexFooterEmpty     = "Use upsert_project_fact to write a fact; call get_project_fact(fact_key) for details."
)

// BuildFactIndexBlock 为 Agent 系统提示生成项目黑板索引（key + summary + 关系边 + 攻击路径，不含 body）。
func BuildFactIndexBlock(db *database.DB, projectID string, cfg config.ProjectConfig) (string, error) {
	if db == nil || !cfg.Enabled {
		return "", nil
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "", nil
	}

	proj, err := db.GetProject(projectID)
	if err != nil {
		return "", err
	}

	facts, err := db.ListProjectFactsForIndex(projectID, cfg.DefaultInjectDeprecated)
	if err != nil {
		return "", err
	}
	allEdges, _ := db.ListProjectFactEdgesByProject(projectID)
	_, incomingByTarget := indexEdgeGroupMaps(allEdges)

	if len(facts) == 0 {
		return wrapFactIndexBlock(fmt.Sprintf("## Project blackboard index (project: %s, id: %s)\n(No facts yet.)\n%s", proj.Name, proj.ID, factIndexFooterEmpty)), nil
	}

	sortFactsForIndex(facts)

	maxRunes := cfg.FactIndexMaxRunesEffective()
	pathMaxRunes := cfg.FactIndexPathMaxRunesEffective()
	footer := factIndexFooterGetDetail + "\n" + factIndexFooterWriteHint
	footerRunes := len([]rune(footer))
	factsBudget := maxRunes - pathMaxRunes - footerRunes
	if factsBudget < 800 {
		factsBudget = maxRunes - footerRunes
		pathMaxRunes = 0
	}

	indexedKeys := make(map[string]struct{}, len(facts))
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Project blackboard index (project: %s, id: %s)\n", proj.Name, proj.ID))
	used := len([]rune(b.String()))
	omitted := 0

	for _, f := range facts {
		indexedKeys[f.FactKey] = struct{}{}
		line := fmt.Sprintf("- [%s] %s — %s (%s)", f.FactKey, f.Category, strings.TrimSpace(f.Summary), f.Confidence)
		line += FormatFactIndexLinksHint(f.FactKey, incomingByTarget[f.FactKey])
		line += "\n"
		lineRunes := len([]rune(line))
		if used+lineRunes > factsBudget {
			omitted++
			continue
		}
		b.WriteString(line)
		used += lineRunes
	}

	if omitted > 0 {
		b.WriteString(fmt.Sprintf("\n(%d additional facts were omitted from the index; use list_project_facts or search_project_facts to query them.)\n", omitted))
	}

	if pathSection := BuildFactPathOverviewSection(allEdges, indexedKeys, pathMaxRunes); pathSection != "" {
		b.WriteString("\n")
		b.WriteString(pathSection)
	}

	b.WriteString(footer)
	return wrapFactIndexBlock(b.String()), nil
}

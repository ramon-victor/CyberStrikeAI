---
name: cyberstrike-eino-demo
description: Full-featured example skill package: SKILL.md plus optional scripts/, references/, assets/, and related directories; validates Eino skill and HTTP in-package paths for authorized security testing and education.
---

# CyberStrike × Eino Full-Feature Skill Demo

This package follows [Agent Skills](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview): **`SKILL.md` is the manifest plus main instruction file** (there is no separate `SKILL.yaml`). The same directory may contain optional subdirectories such as **`scripts/`**, **`references/`**, and **`assets/`** as long as paths are safe and package depth/file-count limits are not exceeded. They can be read through **`ListPackageFiles` / `resource_path`** and Eino local tools. See `FORMS.md` and `REFERENCE.md` for supplementary notes.

## Overview

Used to validate in one pass:

- HTTP `GET /api/skills` listing (`script_count`, `file_count`, `progressive`, and similar fields are derived/scanned results).
- `GET /api/skills/cyberstrike-eino-demo?depth=summary|full`.
- `section=` matching **`##` headings** or ASCII heading short IDs in `SKILL.md` (for example, `## Payload Samples` commonly maps to `section=payload`).
- Multi-agent ADK **`skill`** tool reading package-relative resources, with optional local file tools.
- Eino `FilesystemSkillsRetriever` retrieval of package summaries, `##` chunks, and script entries.

**Hard requirement**: every test must have written authorization and be limited to the agreed scope and time window.

## Authorized Testing Workflow

1. **Scope confirmation**: domains / IPs, API list, and prohibited actions such as DoS or bulk data extraction.
2. **Baseline recording**: perform read-only probing on agreed assets and save timestamps plus raw request/response summaries.
3. **Categorized testing**: split tasks by vulnerability type; reconfirm authorization boundaries before high-risk operations.
4. **Evidence and report**: attach reproduction steps, impact, and remediation to each finding; redact sensitive data.
5. **Closeout**: delete temporary accounts, clean test data, and hand over the report.

## Payload Samples

The following are **educational placeholders**. Replace them with target-specific context during real testing and do not use them against unauthorized systems:

- SQLi probe (error-based): `"'` (observe whether database error information leaks).
- XSS reflected probe (harmless): `<script>alert(1)</script>` -> in a lab, this should be encoded or blocked by CSP.

## Resource Files

- `FORMS.md`: example checklist.
- `REFERENCE.md`: HTTP and runtime reference.
- `references/citations.md`: example external citations.

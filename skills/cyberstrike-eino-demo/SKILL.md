---
name: cyberstrike-eino-demo
description: Full example skill package: SKILL.md + optional scripts/, references/, assets/, and other directories; validates Eino skills and HTTP package paths (authorized security testing and education only).
---

# CyberStrike × Eino Full Skill Demonstration

This package follows [Agent Skills](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview): **`SKILL.md` is the inventory + primary instructions** (there is no separate `SKILL.yaml`). The same directory may contain any subdirectories such as **`scripts/`**, **`references/`**, and **`assets/`** (provided paths are safe and package depth/file-count limits are not exceeded), read by **`ListPackageFiles` / `resource_path`** and Eino local tools. See `FORMS.md` and `REFERENCE.md` for supplemental notes.

## Overview

Use this to verify in one pass:

- HTTP `GET /api/skills` listing (`script_count`, `file_count`, `progressive`, etc. are derived/scanned results)
- `GET /api/skills/cyberstrike-eino-demo?depth=summary|full`
- `section=` maps to a short id for an **`##` heading** or ASCII heading in `SKILL.md` (for example, `## Payload Examples` commonly maps to `section=payload`)
- The multi-agent ADK **`skill`** tool (and optional local file tools) reads package resources by relative path
- Eino `FilesystemSkillsRetriever` retrieves the package summary, `##` chunks, and script entries

**Hard requirement**: obtain written authorization for every test and limit it to the agreed scope and time window.

## Authorized Testing Workflow

1. **Confirm scope**: domains/IPs, interface list, and prohibited actions (DoS, bulk data extraction, etc.).
2. **Record a baseline**: perform read-only probes against agreed assets; preserve timestamps and raw request/response summaries.
3. **Classify testing**: split work by vulnerability type; reconfirm authorization boundaries before high-risk operations.
4. **Evidence and reporting**: attach reproduction steps, impact, and remediation advice to every finding; redact sensitive data.
5. **Close out**: delete temporary accounts, clean test data, and hand over the report.

## Payload Examples

The following are **educational placeholders**; replace them with target context for actual testing and never use them against unauthorized systems:

- SQLi probe (error-based): `"'` (observe whether it triggers database error-information disclosure)
- Reflected XSS (harmless): `<script>alert(1)</script>` → in a lab it should be encoded or blocked by CSP
- Path traversal (read-only verification): `....//....//etc/passwd` (only in an authorized file-read scenario)

See `scripts/payloads.txt` for the full list.

## references/ and assets/

Used to verify that non-`scripts/` subdirectories receive the same treatment:

| Path | Purpose |
|------|------|
| `references/citations.md` | Citation and HTTP `resource_path` test notes |
| `assets/README.txt` | Placeholder resource (may be replaced with a real binary for read-file limit tests) |

## Recommended Toolchain

| Stage | Example tools |
|------|-----------|
| Proxy and replay | Burp Suite, mitmproxy |
| Scanning and directories | ffuf, nuclei (reduce concurrency to comply with authorization) |
| Vulnerability verification | Custom PoC, official CLI (sqlmap, etc.) only within authorized scope |
| Recording | Markdown + JSON snippet template (see `scripts/report-snippet.json`) |

## Inventory and Verification

- [ ] Written authorization and testing window saved
- [ ] Files under `scripts/` match references in the body
- [ ] Index can be checked through Web or `GET /api/skills?...`; load by package with the **`skill`** tool in multi-agent sessions to save tokens
- [ ] When details are needed, pull the full text through **`skill`**, or use HTTP `depth=full`, `section=<heading or short id>`
- [ ] When raw script text is needed, use local file tools or HTTP `resource_path=scripts/check-env.sh`
- [ ] `resource_path=references/citations.md` and `resource_path=assets/README.txt` are readable

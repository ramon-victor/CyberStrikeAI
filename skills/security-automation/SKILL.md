---
name: security-automation
description: Professional skills and methodology for security automation
version: 1.0.0
---

# Security Automation

## Overview

Security automation uses scripts, workflows, and integrations to make assessment, detection, triage, reporting, and remediation repeatable and auditable.

## Use Cases

- Asset inventory and enrichment.
- Vulnerability scanning orchestration.
- Log and alert triage.
- Evidence collection.
- Report generation.
- Configuration compliance checks.
- Secret and dependency scanning.
- Regression verification after remediation.

## Design Principles

- Keep automation scoped and idempotent.
- Prefer read-only operations unless a change is explicitly authorized.
- Record inputs, outputs, timestamps, and tool versions.
- Fail safely and surface partial results.
- Avoid hiding errors or silently skipping targets.
- Separate collection, analysis, and reporting.
- Store secrets securely and avoid logging them.

## Workflow

1. Define objective, target scope, data sources, and success criteria.
2. Choose the smallest reliable automation unit.
3. Validate inputs and normalize target lists.
4. Run tools with explicit parameters and rate controls.
5. Parse outputs into structured data.
6. Deduplicate and rank results.
7. Generate evidence bundles and remediation tasks.
8. Add regression checks.

## Python Patterns

- Use `subprocess.run([...], capture_output=True, text=True, timeout=...)` with argument arrays.
- Stream large files instead of loading everything into memory.
- Use JSON/CSV output modes from tools when available.
- Validate schemas before using parsed results.
- Redact secrets before printing or storing.

## Evidence Requirements

- Automation purpose and scope.
- Tool versions and parameters.
- Input target list hash or path.
- Structured result summary.
- Raw evidence references.
- Errors and skipped items.

## Remediation and Maintenance

- Keep scripts small and version-controlled.
- Add tests for parsers and edge cases.
- Monitor for tool output format changes.
- Review permissions used by automation accounts.
- Document safe rollback for any write operation.

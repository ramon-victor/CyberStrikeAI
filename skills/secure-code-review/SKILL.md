---
name: secure-code-review
description: Professional skills and methodology for secure code review
version: 1.0.0
---

# Secure Code Review

## Overview

Secure code review identifies vulnerabilities in source code, configuration, dependencies, and architecture before or alongside runtime testing.

## Review Scope

- Authentication and session management.
- Authorization and object ownership.
- Input validation and output encoding.
- Injection risks.
- File handling and deserialization.
- Cryptography and secrets.
- Error handling and logging.
- Dependency and supply-chain risk.
- Configuration and deployment.

## Methodology

1. Understand architecture, trust boundaries, data flows, and sensitive assets.
2. Identify entry points: routes, handlers, jobs, CLI, webhooks, parsers.
3. Trace user-controlled data to security-sensitive sinks.
4. Review authorization decisions near data access and state changes.
5. Check configuration, secrets, and dependency manifests.
6. Confirm findings with unit tests, static analysis, or targeted runtime checks where appropriate.
7. Provide precise remediation and regression tests.

## Evidence Requirements

- File path and line/function.
- Source and sink/data flow.
- Vulnerable code snippet.
- Exploitability conditions.
- Impact and affected users/data.
- Recommended fix and test.

## Remediation Principles

- Centralize authorization and validate object ownership.
- Use framework-provided safe APIs.
- Validate inputs with schemas and allowlists.
- Encode output for the correct context.
- Avoid unsafe deserialization and shell execution.
- Store secrets outside code and rotate exposed secrets.
- Add security-focused tests for fixed behavior.

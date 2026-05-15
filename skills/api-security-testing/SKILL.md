---
name: api-security-testing
description: Professional skills and methodology for API security testing
version: 1.0.0
---

# API Security Testing

## Overview

API security testing verifies that API interfaces enforce authentication, authorization, input validation, data protection, and business rules correctly. Use this skill to plan and execute authorized API security assessment with evidence-driven findings.

## Testing Scope

### 1. Authentication and Authorization

**Test items:**
- Token validity verification
- Token expiration handling
- Permission controls
- Role/tenant permission validation
- Session revocation and refresh-token behavior

### 2. Input Validation

**Test items:**
- Parameter type validation
- Length and boundary limits
- Special-character handling
- SQL injection defenses
- XSS defenses
- JSON/XML parser behavior

### 3. Business Logic

**Test items:**
- Workflow bypass
- IDOR / broken object-level authorization
- Rate-limit bypass
- Replay attacks
- Duplicate submission
- State-machine violations

### 4. API Architecture

**Test items:**
- Endpoint discovery
- Deprecated or shadow APIs
- GraphQL introspection and query controls
- OpenAPI/Swagger schema exposure
- Error-message leakage
- CORS and cache behavior

## Methodology

1. Collect API documentation, schemas, base URLs, authentication method, roles, and test accounts.
2. Build an endpoint inventory grouped by function, sensitivity, and authentication state.
3. Establish a baseline with valid requests and expected responses.
4. Mutate authentication, authorization, parameters, methods, headers, and object identifiers.
5. Compare responses across roles and tenants.
6. Preserve evidence: request, response, status code, relevant headers, payload, account role, and timestamp.
7. Report impact with reproducible steps and remediation guidance.

## Recommended Tools

- API clients and proxy tools for request editing/replay.
- GraphQL scanners and schema analyzers.
- JWT analyzers.
- Fuzzers for parameters, methods, and content types.
- Rate-limit and replay testing helpers.

## Evidence Requirements

- Full request/response pairs with secrets redacted.
- The account/role/tenant used.
- Expected behavior versus observed behavior.
- Business impact and affected data/actions.
- Minimal proof that avoids unnecessary sensitive-data exposure.

## Remediation

- Enforce server-side authorization on every object and action.
- Validate all inputs by type, length, format, and allowlist.
- Use short-lived tokens, revocation, audience/issuer checks, and secure refresh flows.
- Disable unnecessary introspection and documentation in production.
- Add rate limits, replay protection, and audit logging for sensitive operations.

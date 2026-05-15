---
name: sql-injection-testing
description: Professional skills and methodology for SQL injection testing
version: 1.0.0
---

# SQL Injection Testing

## Overview

SQL injection occurs when user input changes the structure of a database query. Impact can include authentication bypass, data exposure, data modification, or code execution depending on database and permissions.

## Testing Method

1. Identify parameters in URLs, forms, JSON bodies, headers, cookies, and GraphQL variables.
2. Establish baseline responses.
3. Test syntax indicators with safe payloads such as quotes, parentheses, and type changes.
4. Check boolean-based differences.
5. Check time-based behavior with small delays where allowed.
6. Check error-based leakage.
7. Determine DBMS hints from errors, functions, or timing behavior.
8. Confirm impact minimally and avoid unnecessary data extraction.

## Payload Classes

```text
'
"
')
1 OR 1=1
1 AND 1=2
SLEEP(2)
```

Adapt syntax to the suspected DBMS and authorization scope.

## Evidence Requirements

- Vulnerable parameter and endpoint.
- Baseline request/response.
- Test payload and changed response.
- DBMS indicator if available.
- Minimal proof of impact.
- Remediation guidance.

## Remediation

- Use parameterized queries/prepared statements.
- Avoid string concatenation for SQL.
- Apply allowlist validation for identifiers and sorting parameters.
- Use least-privilege database accounts.
- Hide detailed database errors from users.
- Add regression tests for injection cases.

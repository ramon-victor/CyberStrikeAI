---
name: xpath-injection-testing
description: Professional skills and methodology for XPath injection testing
version: 1.0.0
---

# XPath Injection Testing

## Overview

XPath injection occurs when user input is inserted into XPath expressions without safe handling, allowing query manipulation against XML data sources.

## Vulnerability Principle

Unsafe expression construction such as:

```java
String query = "/users/user[username='" + user + "' and password='" + pass + "']";
```

may allow an attacker to alter the XPath logic.

## Common Inputs

- Login fields backed by XML.
- Search functions.
- Configuration lookup.
- SOAP/XML APIs.
- SAML or XML-heavy integrations.
- Legacy applications using XML databases.

## Testing Method

1. Identify XML-backed features and endpoints.
2. Capture valid and invalid baseline responses.
3. Test syntax-breaking characters such as `'`, `"`, `]`, `)`, and logical operators.
4. Compare boolean differences: true versus false conditions.
5. Test blind cases through response length, timing, or error messages.
6. Avoid extracting unnecessary XML data; confirm with minimal proof.
7. Document the affected expression context if inferable.

## Payload Classes

```text
' or '1'='1
' and '1'='2
') or ('1'='1
count(/*)
```

Adapt only within the authorized target context.

## Evidence Requirements

- Endpoint and parameter.
- Baseline and injected requests.
- Response or behavior difference.
- Inferred XML data exposure or authentication impact.
- Remediation guidance.

## Remediation

- Use safe XPath variable binding or parameterization.
- Avoid string concatenation in XPath.
- Validate input against strict allowlists.
- Return generic errors.
- Limit XML data-store privileges.

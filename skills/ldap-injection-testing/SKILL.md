---
name: ldap-injection-testing
description: Professional skills and methodology for LDAP injection vulnerability testing
version: 1.0.0
---

# LDAP Injection Testing

## Overview

LDAP injection occurs when user input is inserted into LDAP filters or distinguished names without safe escaping, allowing authentication bypass, data exposure, or query manipulation.

## Vulnerability Principle

Unsafe filter construction such as:

```java
String filter = "(&(uid=" + user + ")(userPassword=" + pass + "))";
```

may allow special characters to alter the LDAP query.

## Common Inputs

- Login username/password
- User search
- Directory lookup
- Group or role filter
- Email/phone search
- SSO or enterprise identity integrations

## Testing Method

1. Identify LDAP-backed features.
2. Capture baseline valid and invalid requests.
3. Test LDAP metacharacters safely: `*`, `(`, `)`, `\`, NUL-equivalent encodings, and logical operators.
4. Compare authentication, result count, timing, and error differences.
5. Test blind cases with controlled differences rather than destructive actions.
6. Preserve evidence and avoid extracting unnecessary directory data.

## Example Payload Classes

```text
*
*)(uid=*
)(|(uid=*))
admin*
```

Use only in authorized contexts and adapt to the target syntax.

## Evidence Requirements

- Input point and request.
- Payload class used.
- Response difference or authentication/result change.
- Directory data exposure, if any, redacted.
- Impact and affected identity store.

## Remediation

- Use LDAP parameterization or safe filter builders.
- Escape all LDAP filter and DN metacharacters.
- Enforce least-privilege bind accounts.
- Return generic errors for authentication.
- Monitor unusual directory queries.

---
name: xss-testing
description: Professional skills and methodology for XSS testing
version: 1.0.0
---

# XSS Testing

## Overview

Cross-Site Scripting (XSS) occurs when untrusted input is rendered in a browser without proper context-aware encoding or sanitization.

## Types

- Reflected XSS.
- Stored XSS.
- DOM-based XSS.
- Blind XSS.
- Mutation XSS.

## Testing Method

1. Identify input points and output locations.
2. Establish reflection/storage behavior.
3. Determine output context: HTML body, attribute, JavaScript, CSS, URL, or DOM sink.
4. Use harmless proof payloads.
5. Test encoding, sanitization, CSP, and template behavior.
6. Confirm impact without stealing real cookies or sensitive data.
7. Preserve request/response, rendered context, and screenshot if useful.

## Example Payload Classes

```html
<script>alert(1)</script>
"><svg onload=alert(1)>
javascript:alert(1)
```

Use only authorized targets and adapt to context.

## Evidence Requirements

- Input parameter and sink location.
- Payload and rendered output.
- Browser execution proof.
- Affected user role and exploit conditions.
- Business impact and remediation.

## Remediation

- Apply context-aware output encoding.
- Sanitize rich HTML with a proven library.
- Avoid dangerous DOM sinks such as `innerHTML` with untrusted data.
- Use CSP as defense-in-depth.
- Validate inputs and enforce safe URL schemes.

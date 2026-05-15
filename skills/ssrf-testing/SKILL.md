---
name: ssrf-testing
description: Professional skills and methodology for SSRF testing
version: 1.0.0
---

# SSRF Testing

## Overview

SSRF (Server-Side Request Forgery) occurs when a server fetches attacker-controlled URLs or resources, enabling access to internal services, metadata endpoints, or restricted networks.

## Common Entry Points

- URL preview/fetch features.
- Webhooks.
- Image/document import.
- PDF generation.
- XML parsers.
- File upload by URL.
- Proxy or callback configuration.
- Cloud integrations.

## Testing Method

1. Identify parameters or fields that cause server-side fetching.
2. Use an authorized controlled endpoint to confirm outbound requests.
3. Test scheme handling: `http`, `https`, redirects, DNS rebinding where allowed, and blocked schemes.
4. Test host allowlist bypasses carefully: redirects, encoded IPs, IPv6, localhost aliases, and DNS names.
5. Check cloud metadata access only when explicitly in scope and with harmless metadata paths.
6. Avoid scanning internal networks unless authorized.
7. Preserve callback logs and request evidence.

## Safe Proofs

- DNS or HTTP callback to a controlled domain.
- Fetching a harmless internal health endpoint when explicitly allowed.
- Demonstrating blocked metadata controls without extracting sensitive credentials.

## Evidence Requirements

- Entry point request.
- Controlled callback log or fetched content.
- Redirect or bypass chain if relevant.
- Affected network reachability and impact.
- Safety constraints and remediation.

## Remediation

- Use strict allowlists for destinations.
- Resolve and validate hosts after redirects and DNS changes.
- Block private, loopback, link-local, and metadata ranges.
- Restrict schemes and ports.
- Use egress proxies and network segmentation.
- Disable redirects or revalidate every hop.

---
name: network-penetration-testing
description: Professional skills and methodology for network penetration testing
version: 1.0.0
---

# Network Penetration Testing

## Overview

Network penetration testing evaluates hosts, services, protocols, segmentation, authentication, and exposure within an authorized network scope.

## Testing Scope

- Host discovery.
- Port and service enumeration.
- Version and protocol fingerprinting.
- Misconfiguration assessment.
- Weak authentication.
- Known vulnerability mapping.
- Segmentation and firewall validation.
- Evidence-based exploitation or safe proof where allowed.

## Methodology

1. Confirm network ranges, exclusions, time window, rate limits, and ROE.
2. Discover live hosts with approved techniques.
3. Enumerate ports and services.
4. Fingerprint versions and configurations.
5. Map findings to known vulnerabilities and misconfigurations.
6. Validate high-priority hypotheses with minimal-impact checks.
7. Document evidence, impact, and remediation.

## Service Focus Areas

- SSH, RDP, SMB, FTP, Telnet, SNMP.
- HTTP/HTTPS applications and admin panels.
- Databases and message queues.
- VPN, directory, and identity services.
- Kubernetes, Docker, and cloud metadata endpoints.
- Industrial or specialized protocols only when explicitly in scope.

## Evidence Requirements

- Target range/host and timestamp.
- Tool and parameters used.
- Open ports/services and banners.
- Validation evidence and risk.
- Rate/impact controls.
- Remediation and verification steps.

## Remediation

- Close or restrict unnecessary services.
- Patch vulnerable versions.
- Enforce strong authentication and MFA.
- Segment sensitive networks.
- Disable insecure protocols.
- Monitor and alert on suspicious access.

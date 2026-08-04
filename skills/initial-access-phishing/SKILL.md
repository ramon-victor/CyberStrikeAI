---
name: initial-access-phishing
description: >-
  Initial access/phishing/social engineering: credential spraying, AiTM, device codes, OAuth consent phishing, payloads, and vishing. Use when needing initial access, phishing, AiTM, device code, or social engineering.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Initial Access / Phishing / Social Engineering

```
=== Initial access/phishing/social engineering (the first hop from external to internal) ===
Credential spraying: weak passwords (Season+Year!/companyname123) against all accounts (1-2 attempts per account to avoid lockout) | sources: breach databases/OSINT email formats (f.last@)/default credentials
AiTM phishing (MFA bypass): evilginx3/Modlishka reverse-proxy the real site → steal session cookies+tokens in the middle (bypass MFA) | configure the domain+certificate in a phishlet
Device-code phishing (Azure/M365): device-code flow → induce the victim to enter the code → obtain access/refresh tokens (no password/MFA required) | TokenTactics/AADInternals
Phishing payloads: send with gophish+landing page | payloads lnk/iso/macros/HTA/OneNote | bypass gateways with password-protected zips/cloud-drive links/HTML smuggling
OAuth consent phishing (illicit consent): malicious App requests excessive scopes (Mail.Read/offline_access) → victim consents → long-lived access token
Social-engineering preparation: OSINT organization structure/vendors/password habits (LinkedIn/recruiting/GitHub) | pretexts (IT support/vendor/HR) | vishing (deepfake voice)
→ immediately switch to `redteam-opsec` after initial access lands (do not get caught by EDR at the door); feed obtained capabilities into `capability-primitive-search` to assemble chains
```

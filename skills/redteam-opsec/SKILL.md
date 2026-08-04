---
name: redteam-opsec
description: >-
  OPSEC covert-operations discipline: IP blacklist bypass, rate/timing, traffic obfuscation, minimal footprint, anti-forensics, and gradual exposure. Use when maintaining stealth, bypassing IP bans, or planning covert red-team ops.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## OPSEC / Covert-Operations Discipline (evasion / stability / stealth)

```

Core: getting in ≠ staying stable. Discovery resets the operation to zero. Before every action ask, "what does this look like to the defender?"
🚨IP blacklist bypass (try immediately after a ban): X-Forwarded-For: random IP (when the CDN trusts this header, it directly bypasses the application-layer blacklist; when code:10010/blacklist is returned, try XFF/X-Real-IP/CF-Connecting-IP/True-Client-IP one by one)
  Verification: normal request returns the exact blacklist literal "黑名单"+your IP → add XFF and receive normal 200 → include XFF in all subsequent requests
  Advanced: change the XFF value every N requests (avoid banning the new IP too) | some CDNs trust only the first XFF value, others the last
Rate and timing: rate-limit scans (nuclei -rl / nmap -T2 --max-rate / ffuf -p delay) to avoid WAF bans+IDS threshold alerts | low frequency+random jitter for high-risk actions | avoid both business peaks and late night (match target routines and remain inconspicuous)
Traffic obfuscation: resemble normal business (common UA/Referer/legitimate paths) | probe defenses before heavy weapons (process list/known EDR/SIEM agent indicators) → prefer quiet techniques when present; use automated batches only without monitoring
Minimal footprint: prefer in-memory execution without disk writes (DDexec/memfd/reflective loading) | strong webshell password+uncommon path+function masquerading | tunnel over 443/DNS resembling common egress | delete tools immediately after use (/dev/shm ramdisk, leave no residue)
Anti-forensics: command history unset HISTFILE / set +o history | selectively delete your own log entries (truncating everything triggers alerts) | touch -r neighboring files to preserve mtime | do not touch monitoring/audit services (stopping them is an alert)
Gradual exposure: passive recon (Certificate Transparency/passive DNS/search engines/asset engines) → confirm no strong monitoring → active scans → exploitation and foothold last. Never actively touch the target for information available publicly. At every level ask, "is exposure worth it?"
> Stealth is not obsession; it is red-team survivability. One reckless full-port, full-speed scan can reset the entire operation.
```

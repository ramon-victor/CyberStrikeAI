---
name: source-code-hunting
description: >-
  Source-code hunting: .git leaks, dangerous-function grep, JS RC4 deobfuscation, semgrep/CodeQL, trufflehog, patch diffing, and supply-chain/CI issues. Use when hunting source leaks, secrets, JS deobfuscation, or supply-chain issues.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Source-Code Hunting

```
=== Source-code hunting ===
.git leaks: git-dumper → git log -p --all (deleted sensitive files) | .svn/.DS_Store/composer.lock
Dangerous-function grep: exec/system/eval/unserialize/pickle.loads/render/curl_exec + hardcoded keys (sk-/ghp_/BEGIN RSA)
Breaking JS obfuscation (RC4+base64 string-array pattern): 1) extract the string array (var a0G=[...]) 2) find the decode function (a0m(idx,key) → RC4 decrypt+base64) 3) find the rotation IIFE (target offset) 4) rebuild the decoder in Node.js and batch-decode every string → recover plaintext variable names/API paths/configuration
  UniApp indicators: app-service.js (business logic, often 800KB+ and obfuscated) + zlsioh.dat (encrypted configuration, decrypted by native .so) + dcloud_uniplugins.json (plugin inventory)
Static scanning: quick scan with semgrep --config=auto / build a CodeQL database and write queries (see `zero-day-discovery` for taint solving+variant-analysis methodology)
Deep secret hunting: use trufflehog/gitleaks to scan complete git history+Docker image layers+npm/PyPI tarballs+frontend bundles (--only-verified distinguishes live from dead keys)
Framework Patch Diff: clone and diff versions before and after → what was fixed reveals where the vulnerability is | dependency chain: composer.json/npm audit/pip-audit
Supply chain/CI: dependency confusion (register internal package names in public registries) | GHA command injection through ${{github.event.issue.title}} | self-hosted runner takeover | .npmrc/.pypirc credentials
```

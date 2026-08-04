---
name: zero-day-discovery
description: >-
  Autonomous 0day discovery engine: variant analysis/patch gaps/differential testing/fuzzing/taint reasoning/N-day weaponization/hunter mindset. Use when public vulnerabilities are not found and you need to discover a 0day or weaponize an N-day.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---


## Autonomous 0day Discovery Engine (find it yourself when no vulnerability exists online)

```
Core shift: move from "matching known-vulnerability databases" → "understanding how code/protocols work and inferring where they will break." A 0day is methodology, not luck.
Five primary paths:
  1. Variant analysis (highest yield): take a CVE patch → distill the vulnerability pattern → grep the entire codebase for the same pattern elsewhere → anything the patch missed is a 0day
  2. Patch gaps: read the patch's filtering logic; blacklists can almost always be bypassed (a missed encoding/equivalent function/alias) → new CVE
  3. Differential testing: two components interpret the same input differently (WAF vs backend/validator vs executor) → request smuggling/SSRF bypass
  4. Fuzzing: write a harness (wrap the function that handles untrusted input)+build a corpus/dictionary+triage crashes (reproducibility/control/exploitability)
     AFL++/libFuzzer (coverage-guided memory-corruption discovery) boofuzz (protocols) radamsa (black box) restler (REST API)
  5. Taint reasoning (strongest with source): source (parameter/Header/deserialized field) reaches a sink (exec/SQL/template) without an effective sanitizer = 0day
     Write CodeQL queries to solve data-flow reachability automatically / Semgrep / Joern
N-day weaponization (an advisory exists but there is no public PoC): reconstruct the exploit from the patch diff (bindiff/diaphora binary comparison,
  vendor regression tests are often embryonic PoCs) → make it work in a local test environment → attack the target. Maximizing the vulnerability window is one of the most valuable red-team capabilities.
Hunter mindset (interrogate every piece of code/endpoint/protocol layer by layer; every "yes" is a 0day candidate):
  Trust boundary: is the assumed input trustworthy? When does that fail? | state timing: can state change between two steps (TOCTOU)? Can the order be disrupted?
  Parsing and normalization: how many times is it parsed? Is normalize applied before or after validation? | boundary extremes: negative/0/huge/type confusion/encoding/null byte?
  Implicit capability: what does this feature give me "as a side effect"? | uniqueness: is the ID/token predictable? Is the "secret" actually secret?
Turn the component into an "assumption checklist" and break each assumption: file upload assumes "only images are uploaded/the extension is trustworthy/the filename contains no path/the content is only data" → breaking each one = vulnerability; combining them = chain.
0day validation (higher bar than a CVE): reproducible (minimal PoC)+clear root cause (which line/which assumption)+demonstrable impact (actual read/write/execute)+false positives excluded.
```

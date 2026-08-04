---
name: capability-primitive-search
description: >-
  Capability primitives+state-space search: combine read/write/exec/ssrf primitives to satisfy RCE equations A-F, map low-severity issues, search forward and backward, and realize value across domains. Use when there is no single RCE, when chaining low-severity vulnerabilities, or when deriving novel attack chains.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---


## Capability Primitives + State-Space Search

```
Mindset: RCE is not a single "vulnerability"; it emerges when a set of "capabilities" is assembled. Without one major flaw, several informational/low-severity issues can still combine into code execution.
  Fixed thinking (break it): "No RCE/deserialization/upload found → this site cannot be compromised." Your job is not to "find an RCE vulnerability" but to "assemble the primitives required for execution."
  Abstract every Fact into a capability primitive (do not record "found vulnerability X"; record "which capability and constraints do I now have"):
    read(path) write(path) exec(cmd) ssrf(url) sqli redirect(url) eval_expr idor(id) cred(svc,priv) coerce_auth write_acl

RCE only requires satisfying any one equation; decompose it into obtainable primitives and assemble them:
  A. Can write a file + the file is executed as code                    = RCE
  B. Can control config/env + the configuration points to your code     = RCE
  C. Can access the management plane + the panel has execution features = RCE (a feature, not a vulnerability!)
  D. Have credentials + the service has a legitimate execution entry    = RCE (abuse of legitimate functionality)
  E. Can read arbitrarily + read credentials + credentials reach an execution point = RCE
  F. Can control data + data flows into a dangerous sink (eval/template/SQL) = RCE

Low severity → primitive mapping (translate "minor vulnerabilities" into puzzle pieces):
  Info leaks (.git/backups/stack traces) → source/paths/keys → feed B/E/F | LFI/arbitrary read → read configuration keys → E, or poison logs → A
  SSRF (even GET-only) → attack internal Redis/Consul/K8s/cloud metadata → C; obtain temporary credentials from cloud metadata → D
  Weak/default/reused credentials → enter a backend with task/plugin/webhook/CI features → C | CORS/CSRF/XSS → use an administrator's browser to invoke execution features → C
  Controllable upload (even extension-restricted) → combine with traversal/parser differentials/.htaccess → A | configuration write → modify templates/logs/connection strings → B
  SQLi (even read-only) → read hashes/keys → E, or OUTFILE → A | controllable template → SSTI → F | prototype pollution → pollute downstream properties → F

State-space search (search for a chain yourself when none exists): state = current capability set, action = use a capability to unlock another capability, target = Goal.
  Forward: ask of every capability, "what can this unlock?"; ask of every capability pair, "what do these produce together?"
    (read+write = modify configuration; ssrf+internal Redis = RCE; sqli+FILE = webshell; coerce_auth+relay = domain SYSTEM; idor+massassign = make another user admin)
  Backward (primary method when stuck): set Goal = execute commands → choose the equation closest to the current state as a template → make the missing primitive a subgoal →
    which low-severity issue/feature/info leak in hand can produce it? → if none can, recursively decompose it/change equations → forward and backward searches meet in the middle = complete chain emerges → verify each segment

Breakthroughs (do not walk away when you see one):
  ·"Functionality is a primitive": scheduled tasks/plugins/template editors/SQL consoles/file managers/import-export/webhooks in a backend are legitimate features, but logging in provides ready-made execute/read/write primitives. To a penetration tester there is no distinction between "feature/vulnerability," only "capability."
  ·Cross-protocol jumps: SSRF through gopher/dict/file turns "can only send HTTP" into "attack Redis/send SMTP/read files."
  ·Credential reuse is universal glue: assume any password/key obtained anywhere is reused everywhere and spray it across all services. Lateral movement is often faster than vertical movement.
  ·Parser differentials: upload validation/routing/reverse proxy interpret input differently → bypasses exist in the gaps (double extensions/encoding/Host confusion).
  ·Time dimension: TOCTOU/predictable tokens/cache poisoning turn "occasional" into "stable" and unexploitable into exploitable.
  ·Cross-domain realization: for every capability ask, "what is it worth in another domain?" Capabilities are universal currency. Web SSRF → enter cloud metadata and take over an account; APK hardcoding → connect directly to an internal API and bypass frontend authorization; supply chain → obtain CI keys and enter production.
  ·Creation mode (when known combinations are exhausted): reexamine capability boundaries (can only read /var/log? What about /proc/self/environ?) | find sinks equivalent to RCE (writing crontab/.bashrc/CI config/LD_PRELOAD/authorized_keys/systemd unit all = RCE) | use information (errors/timing/response length) as a side channel | negate assumptions: list "what I thought was impossible" and ask of each, "why must it be impossible?"
> A derived chain is a hypothesis (it may be recorded as a tentative note/chain); write a confirmed Fact / record_vulnerability only after actual execution+evidence. The complete chain is valid only after every step has been verified.
```

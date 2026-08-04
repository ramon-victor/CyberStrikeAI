---
name: active-directory-attack
description: >-
  Internal domain attacks: BloodHound, Kerberoast, ADCS ESC1/ESC8, NTLM Relay, Coerce, DACL, DCSync, Zerologon/NoPac/PrintNightmare, mitm6, LLMNR, and Linux internal networks. Use when attacking Active Directory, ADCS, NTLM relay, or an internal domain.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Internal Domain Attacks

```
=== Internal domains (the real primary battleground since 2023) ===
Recon: BloodHound (SharpHound collection → attack paths) | Kerberos: GetNPUsers (AS-REP)/GetUserSPNs (Kerberoast)
🚨ADCS (use Certipy for everything): certipy find -vulnerable | ESC1: specify a SAN to request a domain-admin certificate | ESC8: relay to the CA to obtain a DC certificate
🚨NTLM Relay (more important than PtH, which EDR often blocks): ntlmrelayx -t ldap--escalate-user / -t http CA --adcs(ESC8) / RBCD
🚨Coerce authentication: PetitPotam (MS-EFSRPC)/coercer against all protocols/printerbug → feed into relay
🚨DACL abuse: WriteDACL → grant yourself DCSync | Shadow Credentials with certipy shadow (GenericWrite is sufficient; no password change and no obvious trace)
DCSync: secretsdump -just-dc → krbtgt hash→Golden Ticket
🚨One-shot domain CVEs (test first; a hit yields domain admin directly): Zerologon (CVE-2020-1472, blank the DC machine-account password → DCSync) | NoPac (CVE-2021-42278/42287, rename a machine account and request a DC TGT) | PrintNightmare (CVE-2021-34527, print-spooler RCE/load a malicious driver) | EternalBlue (MS17-010, direct RCE on legacy SMBv1)
🚨IPv6/mitm6 (mandatory in default dual-stack internal networks): mitm6 hijacks DHCPv6+DNS → WPAD → ntlmrelayx to LDAP/ADCS (stealthier than LLMNR and preferred in modern internal networks)
LLMNR/NBT-NS poisoning: responder captures NetNTLMv2 → crack with hashcat/relay
Linux internal networks: unauthenticated Redis (CONFIG SET dir to write an SSH key) | NFS showmount | Docker 2375
```

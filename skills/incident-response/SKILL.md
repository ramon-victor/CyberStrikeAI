---
name: incident-response
description: Professional skills and methodology for incident response
version: 1.0.0
---

# Incident Response

## Overview

Incident response identifies, contains, eradicates, and recovers from security incidents while preserving evidence and minimizing business impact.

## Phases

### 1. Preparation

- Define roles and contacts.
- Establish evidence handling.
- Prepare logging, EDR, SIEM, backup, and communication channels.
- Maintain playbooks and escalation paths.

### 2. Identification

- Triage alerts.
- Establish timeline.
- Identify affected assets, users, data, and attack vectors.
- Classify severity and confidence.

### 3. Containment

- Isolate affected systems where appropriate.
- Disable compromised credentials.
- Block malicious indicators.
- Preserve volatile evidence before disruptive action when required.

### 4. Eradication

- Remove malware, persistence, unauthorized accounts, and malicious changes.
- Patch exploited vulnerabilities.
- Rotate secrets and keys.
- Validate clean state.

### 5. Recovery

- Restore services safely.
- Monitor for recurrence.
- Validate business functionality.
- Communicate status to stakeholders.

### 6. Lessons Learned

- Root-cause analysis.
- Control improvements.
- Detection and playbook updates.
- Evidence and report archive.

## Evidence Checklist

- Alert source and timestamps.
- Affected hosts/accounts.
- Relevant logs and telemetry.
- File hashes, network indicators, process trees.
- Actions taken and owners.
- Business impact and recovery status.

## Output Format

- Executive summary.
- Timeline.
- Scope of impact.
- Containment/eradication/recovery actions.
- Evidence and confidence.
- Recommendations and follow-up tasks.

---
name: cloud-security-audit
description: Professional skills and methodology for cloud security auditing
version: 1.0.0
---

# Cloud Security Audit

## Overview

Cloud security auditing evaluates the security posture of cloud environments. This skill covers authorized assessment methods, tools, and best practices for AWS, Azure, GCP, and other cloud platforms.

## Audit Scope

### 1. Identity and Access Management

**Check items:**
- IAM policy configuration
- User permissions
- Role permissions
- Access-key management
- MFA enforcement
- Privilege escalation paths
- Cross-account trust relationships

### 2. Network Security

**Check items:**
- Security groups and firewall rules
- Network ACLs
- VPC/VNet configuration
- Public IP exposure
- Ingress/egress restrictions
- Private endpoints and peering
- Traffic encryption

### 3. Data Security

**Check items:**
- Storage-bucket/container permissions
- Database exposure
- Encryption at rest and in transit
- Key-management configuration
- Backup and snapshot exposure
- Data-retention policies

### 4. Logging and Monitoring

**Check items:**
- Audit logging enabled
- Centralized log retention
- Alerting rules
- CloudTrail/Activity Log/Audit Logs coverage
- GuardDuty/Security Command Center/Defender findings

### 5. Compute, Container, and Serverless

**Check items:**
- Instance metadata protections
- Publicly exposed workloads
- Container registry permissions
- Kubernetes cluster configuration
- Serverless environment variables and permissions
- Patch and image vulnerability status

## Methodology

1. Confirm accounts/projects/subscriptions, regions, ROE, and read-only credentials.
2. Inventory identities, networks, compute, storage, databases, containers, and serverless functions.
3. Identify public exposure and excessive permissions.
4. Review critical controls: MFA, logging, encryption, key rotation, secrets, and backups.
5. Validate high-risk findings with read-only or minimal-impact checks.
6. Rank risk by exposure, privilege, data sensitivity, and exploitability.
7. Provide remediation and regression-verification steps.

## Evidence Requirements

- Cloud account/project/subscription and region.
- Resource identifiers.
- Policy snippets or configuration evidence.
- Exposure path and affected principals.
- Impact explanation with least-privilege remediation.

## Remediation

- Apply least privilege and role separation.
- Remove public access unless explicitly required.
- Enforce MFA and conditional access.
- Enable and retain audit logs.
- Encrypt sensitive data and rotate keys/secrets.
- Use infrastructure-as-code policy checks and continuous posture monitoring.

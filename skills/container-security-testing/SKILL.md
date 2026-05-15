---
name: container-security-testing
description: Professional skills and methodology for container security testing
version: 1.0.0
---

# Container Security Testing

## Overview

Container security testing verifies the security of containerized applications and orchestration platforms such as Docker and Kubernetes.

## Testing Scope

### 1. Image Security

**Check items:**
- Base-image vulnerabilities
- Dependency vulnerabilities
- Image configuration
- Embedded sensitive information
- Unnecessary tools and packages
- Image signing and provenance

### 2. Runtime Security

**Check items:**
- Container privileges
- Linux capabilities
- Resource limits
- Network isolation
- Filesystem permissions
- Read-only root filesystem
- Host mounts and Docker socket exposure

### 3. Orchestration Security

**Check items:**
- Kubernetes configuration
- RBAC and service accounts
- Pod Security Standards
- Network policies
- Secrets handling
- Admission controllers
- Ingress exposure

### 4. Supply Chain

**Check items:**
- Registry permissions
- CI/CD secrets
- Build context leakage
- SBOM availability
- Dependency pinning
- Vulnerability scanning gates

## Methodology

1. Inventory images, registries, clusters, namespaces, workloads, and exposed services.
2. Scan images and dependencies for known vulnerabilities.
3. Review Dockerfiles and manifests for hardening gaps.
4. Check runtime privileges, mounts, capabilities, and namespaces.
5. Review Kubernetes RBAC, network policies, secrets, ingress, and admission controls.
6. Validate findings with read-only or minimal-impact evidence.
7. Prioritize by exposure, privilege, exploitability, and data sensitivity.

## Evidence Requirements

- Image digest/tag and registry.
- Kubernetes namespace/workload/resource identifiers.
- Misconfiguration snippets.
- Vulnerability IDs and affected packages.
- Proof of exposure or privilege risk.
- Remediation and regression checks.

## Remediation

- Use minimal, patched, signed images.
- Drop unnecessary capabilities and avoid privileged containers.
- Use non-root users and read-only filesystems.
- Restrict hostPath mounts and Docker socket access.
- Enforce RBAC least privilege and network policies.
- Store secrets in approved secret managers and rotate exposed secrets.

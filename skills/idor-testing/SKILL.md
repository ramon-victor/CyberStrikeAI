---
name: idor-testing
description: Professional skills and methodology for IDOR and broken object-level authorization testing
version: 1.0.0
---

# IDOR Testing

## Overview

IDOR (Insecure Direct Object Reference) and BOLA (Broken Object-Level Authorization) occur when an application exposes object identifiers and fails to verify that the current user is authorized to access or modify the referenced object.

## Common Targets

- User profiles
- Orders and invoices
- Tickets and messages
- Documents and uploads
- API resources
- Admin actions
- Organization or tenant resources

## Testing Method

1. Obtain at least two authorized accounts with different owners, roles, or tenants.
2. Capture baseline requests for object read, create, update, delete, and workflow actions.
3. Identify object identifiers in paths, query parameters, JSON bodies, headers, and GraphQL variables.
4. Swap identifiers between accounts and roles.
5. Test predictable IDs, UUIDs, encoded IDs, composite IDs, and nested object references.
6. Compare response codes, bodies, side effects, and audit logs where available.
7. Confirm impact with minimal safe operations and redacted data.

## Test Cases

- Read another user's object.
- Modify another user's object.
- Delete or cancel another user's object.
- Access hidden attachments or exports.
- Cross-tenant access.
- Role downgrade/upgrade mismatch.
- Bulk operations containing unauthorized IDs.
- GraphQL node/global ID access.

## Evidence Requirements

- Account A and Account B context.
- Original authorized request.
- Modified request with swapped ID.
- Response and side effect.
- Data sensitivity and business impact.
- Clear remediation target.

## Remediation

- Enforce object-level authorization on every request server-side.
- Check ownership and tenant membership at the service layer.
- Avoid trusting client-supplied owner, tenant, or role fields.
- Use consistent authorization middleware and tests.
- Add audit logging for denied and sensitive object access.

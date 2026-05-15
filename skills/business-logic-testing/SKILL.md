---
name: business-logic-testing
description: Professional skills and methodology for business logic vulnerability testing
version: 1.0.0
---

# Business Logic Vulnerability Testing

## Overview

Business logic vulnerabilities are design flaws in application workflows. They can cause unauthorized actions, data tampering, fraud, privilege misuse, or financial loss even when the underlying code has no obvious injection bug.

## Vulnerability Types

### 1. Workflow Bypass

**Examples:**
- Directly accessing a final step.
- Reordering steps.
- Repeating a step that should be one-time.
- Skipping approval, payment, verification, or review gates.

### 2. Price and Amount Manipulation

**Examples:**
- Negative prices or quantities.
- Client-side price changes.
- Currency/precision rounding abuse.
- Discount, coupon, tax, shipping, or refund manipulation.

### 3. Quantity and Limit Bypass

**Examples:**
- Negative quantities.
- Exceeding purchase, withdrawal, or rate limits.
- Race conditions on counters.
- Multiple accounts or sessions bypassing per-user controls.

### 4. Authorization and Ownership Flaws

**Examples:**
- Accessing another user's order, ticket, invoice, or file.
- Changing object identifiers.
- Crossing tenant boundaries.
- Performing privileged actions with a normal account.

### 5. State and Race Conditions

**Examples:**
- Double spending.
- Duplicate rewards.
- Concurrent checkout or transfer.
- Retrying expired or consumed operations.

## Testing Methodology

1. Understand the business process, actors, roles, state transitions, and financial or data impact.
2. Draw the intended workflow and identify trust boundaries.
3. Capture valid baseline requests for each step.
4. Change step order, repeat operations, alter state fields, and swap object identifiers.
5. Test boundaries: zero, negative, very large, expired, reused, and concurrent values.
6. Compare behavior across roles, tenants, sessions, and devices.
7. Confirm impact with the smallest safe proof and preserve request/response evidence.

## Test Cases

- Can a user access a resource they do not own?
- Can a user skip a mandatory step?
- Can the client modify server-trusted fields?
- Can a completed transaction be replayed?
- Can rate or quantity limits be bypassed through concurrency?
- Can different accounts cooperate to bypass limits?
- Does cancellation/refund/reversal preserve correct state?

## Evidence Requirements

- Workflow diagram or step list.
- Baseline valid flow.
- Modified request(s) and response(s).
- Account/role/tenant context.
- Business impact with minimal data exposure.
- Clear explanation of expected versus actual behavior.

## Remediation

- Enforce state transitions server-side.
- Never trust client-side price, ownership, role, or state fields.
- Use idempotency keys and transaction locking for sensitive operations.
- Enforce object-level authorization and tenant isolation at the service layer.
- Add anomaly detection, audit logs, and regression tests for business rules.

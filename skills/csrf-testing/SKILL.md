---
name: csrf-testing
description: Professional skills and methodology for CSRF testing
version: 1.0.0
---

# CSRF Testing

## Overview

CSRF (Cross-Site Request Forgery) abuses a user's authenticated browser state to perform unauthorized actions. This skill covers detection, safe proof, and remediation.

## Vulnerability Principle

- An attacker induces the user to visit a malicious page.
- The page sends a request to the target site automatically.
- The browser includes existing authentication information such as cookies or sessions.
- The target site mistakenly treats the request as a legitimate user action.

## Testing Method

### 1. Identify Sensitive Operations

- Password changes
- Email changes
- Transfers or purchases
- Permission changes
- Data deletion
- State updates

### 2. Check CSRF Token Protection

```html
<!-- Token-protected example -->
<form method="POST" action="/change-password">
  <input type="hidden" name="csrf_token" value="abc123">
  <input type="password" name="new_password">
</form>
```

Assess whether the token exists, is tied to the user/session, changes appropriately, and is validated server-side.

### 3. Test SameSite and Origin Controls

- Cookie `SameSite` attribute.
- `Origin` and `Referer` validation.
- CORS configuration.
- Method and content-type restrictions.

### 4. Proof of Concept

Use a benign state change in an authorized account and avoid destructive actions.

```html
<form action="https://target.example/change-email" method="POST">
  <input type="hidden" name="email" value="csrf-test@example.com">
</form>
<script>document.forms[0].submit()</script>
```

## Evidence Requirements

- Sensitive endpoint and request.
- Token/origin/cookie behavior.
- PoC HTML or request sequence.
- Expected versus observed state change.
- Account used and safety controls.

## Remediation

- Use unpredictable per-session or per-request CSRF tokens.
- Validate tokens server-side for every state-changing request.
- Set cookies to `SameSite=Lax` or `Strict` where feasible.
- Validate `Origin`/`Referer` for state-changing requests.
- Require re-authentication or step-up verification for critical actions.

---
name: mobile-app-security-testing
description: Professional skills and methodology for mobile application security testing
version: 1.0.0
---

# Mobile Application Security Testing

## Overview

Mobile application security testing evaluates Android/iOS applications, their local storage, network communication, backend APIs, and platform integrations.

## Testing Scope

### 1. Static Analysis

- Manifest / Info.plist review.
- Permissions and exported components.
- Hardcoded secrets.
- Insecure cryptography.
- Debug flags and logging.
- Third-party SDKs and dependency versions.

### 2. Dynamic Analysis

- Runtime behavior.
- Local storage.
- Network requests.
- Authentication and session handling.
- Certificate pinning behavior.
- Root/jailbreak detection.

### 3. Backend API Testing

- API authentication and authorization.
- IDOR/BOLA.
- Rate limits.
- Business logic.
- Error leakage.

### 4. Platform Security

- Android intents, activities, services, receivers, content providers.
- iOS URL schemes, keychain, pasteboard, app groups.
- Deep links and universal/app links.
- Push notifications.

## Methodology

1. Confirm application package, version, environment, test accounts, and ROE.
2. Perform static analysis on the APK/IPA and configuration files.
3. Run the app in a controlled device/emulator.
4. Capture and analyze network traffic where allowed.
5. Test local storage and sensitive-data handling.
6. Validate backend API security with role/tenant comparisons.
7. Document findings with reproducible evidence and remediation.

## Evidence Requirements

- App identifier, version, platform, and environment.
- Affected file/component/API.
- Request/response or runtime proof.
- Screenshots or logs with secrets redacted.
- Business impact and remediation.

## Remediation

- Remove hardcoded secrets and debug code.
- Use secure platform storage for sensitive data.
- Enforce TLS and validate certificates.
- Implement backend authorization server-side.
- Harden exported components and deep links.
- Obfuscate where appropriate but do not rely on obfuscation alone.

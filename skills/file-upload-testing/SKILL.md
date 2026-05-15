---
name: file-upload-testing
description: Professional skills and methodology for file upload vulnerability testing
version: 1.0.0
---

# File Upload Vulnerability Testing

## Overview

File upload vulnerabilities arise when applications accept, store, process, or serve uploaded files without sufficient validation and isolation. Impact may include remote code execution, stored XSS, malware hosting, path traversal, or sensitive-file exposure.

## Testing Scope

- Extension allowlist and denylist behavior.
- MIME type and magic-byte validation.
- File size, count, and rate limits.
- Filename handling and path traversal.
- Storage location and execution permissions.
- Image/document processing pipeline.
- Public access controls and object ownership.
- Antivirus or content scanning behavior.

## Test Cases

### 1. Extension and Content Mismatch

- Valid extension with invalid content.
- Dangerous extension with benign content.
- Double extensions such as `file.php.jpg`.
- Case and Unicode variations.

### 2. MIME and Magic Bytes

- Change `Content-Type`.
- Prefix valid magic bytes to unexpected content.
- Upload polyglot files where appropriate and authorized.

### 3. Filename Handling

- Path separators.
- Long names.
- Special characters.
- Existing filename overwrite.
- Archive extraction paths.

### 4. Execution and Rendering

- Can uploaded files execute server-side?
- Are HTML/SVG files rendered in a script-capable context?
- Are files served with safe `Content-Type` and `Content-Disposition`?

## Evidence Requirements

- Upload request and response.
- Stored URL/path or object identifier.
- Validation bypass method.
- Safe proof of execution/rendering/access control issue.
- Impact and affected file types.

## Remediation

- Use strict allowlists for file type, extension, content, and size.
- Store uploads outside executable web roots.
- Generate server-side filenames and strip path characters.
- Serve untrusted files with safe content types and attachment disposition.
- Scan content and archives.
- Enforce per-user authorization on download/access.

---
name: command-injection-testing
description: Professional skills and methodology for command injection vulnerability testing
version: 1.0.0
---

# Command Injection Vulnerability Testing

## Overview

Command injection occurs when an application passes user-controlled input to an operating-system command without sufficient validation or escaping. Attackers may inject additional commands. Use this skill only in authorized testing and prefer minimal-impact proofs.

## Vulnerability Principle

Applications become vulnerable when user input is concatenated into system commands or shell expressions and executed by the OS.

**Dangerous code example:**
```php
// PHP
system("ping " . $_GET['ip']);
```

**Safer approach:**
```php
$ip = filter_input(INPUT_GET, 'ip', FILTER_VALIDATE_IP);
if ($ip === false) {
    http_response_code(400);
    exit;
}
$cmd = ['ping', '-c', '1', $ip];
```

## Testing Method

### 1. Identify Command Execution Points

**Common features:**
- Ping or traceroute
- DNS lookup
- File operations
- System information
- Log viewing
- Backup/restore
- Image, PDF, or media processing

### 2. Basic Detection

**Command separators to test carefully:**
```text
;   command separator
&   background/command separator depending on shell
&&  execute next command if previous succeeds
||  execute next command if previous fails
|   pipe
`cmd` and $(cmd) command substitution
```

Use harmless commands that prove execution without causing damage, such as `id`, `whoami`, `hostname`, or controlled DNS callbacks where allowed.

### 3. Blind Detection

When output is not reflected:
- Time delay with safe sleep values.
- DNS/HTTP callback to an authorized controlled endpoint.
- File creation only in approved temporary locations when explicitly allowed.

### 4. Filter Bypass Checks

Test only within scope:
- URL encoding and double encoding.
- Whitespace alternatives.
- Environment-variable expansion.
- Shell metacharacter escaping behavior.
- Argument injection when shell metacharacters are blocked.

## Evidence Requirements

- Vulnerable parameter and request.
- Payload used, redacted if necessary.
- Response, delay measurement, callback log, or command output.
- Scope and safety controls.
- Impact statement.

## Remediation

- Avoid shell execution; use safe library APIs.
- Pass arguments as arrays without shell interpolation.
- Strict allowlist validation for values such as IPs, domains, and filenames.
- Run services with least privilege.
- Sandbox command execution and log sensitive operations.

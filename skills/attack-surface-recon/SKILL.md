---
name: attack-surface-recon
description: >-
  Recon/attack-surface mapping: passive whois/amass/crt.sh/FOFA/Shodan; active subfinder/httpx/naabu/katana/nuclei; DNS geography/CDN/Nginx catch-all/BT Panel/UniApp fingerprinting. The first action in an engagement; write knowledge to the project blackboard. Use when starting recon, asset mapping, fingerprinting, or CDN/DNS bypass discovery.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Recon / Attack-Surface Mapping

```
=== Recon/attack-surface mapping (first action; spend 60% of engagement time here and do not rush naked into exploitation; immediately upsert_project_fact for ports/fingerprints/etc.) ===
Start with passive methods (do not touch the target; execute all):
  terminal: whois {domain} | grep -iE 'org|name|email|registrant'
  terminal: amass intel -asn {ASN} -d {domain}
  browser_navigate: https://crt.sh/?q=%.{domain} (mine Certificate Transparency for subdomains)
  terminal: curl -s "https://crt.sh/?q=%.{domain}&output=json" | python3 -c "import sys,json;[print(x['name_value']) for x in json.load(sys.stdin)]" | sort -u
  terminal: dig +short {domain} @114.114.114.114; dig +short {domain} @8.8.8.8 (compare differences = CDN)
  terminal: curl -s "https://web.archive.org/cdx/search/cdx?url=*.{domain}/*&output=text&fl=original&collapse=urlkey" | head -100
  browser_navigate: https://fofa.info/result?qbase64=$(echo -n 'domain="{domain}"' | base64)
  browser_navigate: https://www.shodan.io/search?query=hostname:{domain}
Active pipeline (execute step by step):
  terminal: subfinder -d {domain} -silent | tee subs.txt; amass enum -d {domain} -passive >> subs.txt; sort -u subs.txt -o subs.txt
  terminal: cat subs.txt | dnsx -silent | tee alive_subs.txt
  terminal: cat alive_subs.txt | httpx -silent -title -status-code -tech-detect -cdn | tee httpx_out.txt
  terminal: naabu -l alive_subs.txt -top-ports 1000 -silent | tee ports.txt; nmap -sCV -iL <(head -20 alive_subs.txt) -oN nmap_out.txt
  terminal: katana -list alive_subs.txt -silent -d 3 | tee urls.txt; gau {domain} >> urls.txt
  terminal: cat urls.txt | grep -iE '\.(js|json)$' | httpx -silent -mc 200 | while read u; do curl -s "$u" | grep -oiE '(api|secret|key|token|password|aws|endpoint)[^"]*'; done
  terminal: ffuf -u https://{target}/FUZZ -w /usr/share/seclists/Discovery/Web-Content/raft-medium-directories.txt -mc 200,301,302,403 -fs {catch-all-size}
  terminal: nuclei -l alive_subs.txt -t /root/nuclei-templates/ -severity medium,high,critical -o nuclei_results.txt
  → when httpx output identifies a framework/version → immediately trigger the `component-vuln-intel` search sequence
🚨Bypass DNS geographic restrictions: Chinese CDNs (987dns/dnspod/Alibaba Cloud) often return 0.0.0.0 to overseas DNS; resolution through 114.114.114.114 may be required to obtain the real IP. Compare multiple DNS servers: dig @1.1.1.1 vs @114.114.114.114 vs @8.8.8.8; differences reveal a CDN geographic policy. The real IP may be hidden in Chinese DNS results.
🚨Bypass CDN response filtering (GCCDN/PWS, etc.): the CDN returns 502 for specific URL patterns (admin paths/actuator) instead of forwarding the backend response:
  Fingerprints: Server: PWS/x.x.x.x | Via: 1.1 PS-XXX-XXXX:N (W) | X-Px: ms CS-XXX-XXXXnone(origin) | wildcard *.gccdn.net CNAME
  502 ≠ nonexistent: 502 = CDN filtered the backend response, 404 = path truly does not exist, 403 = CDN route rejected. Use 502/404/403 differences to enumerate live endpoints
  Confirm traversal: /api/v1/v/..;/..;/actuator/env → 502 (exists but filtered) vs /api/v1/v/..;/..;/xxxx → 404 (natively nonexistent in Tomcat)
  Multiple CDN-node ports: scan ports 3000-9200 on CDN IPs (8000/8080/9090 often proxy the same backend under potentially different rules; 9200 may be an independent nginx)
  Bypass-attempt matrix (all failed in this session; record to prevent repetition): URL encoding/double encoding/case changes/semicolon suffix/TE whitespace smuggling/HTTP pipelining/Accept-Encoding/Range
  Extract useful information: even with 502, a POST to actuator/env may still execute on the backend (blind property write) → requires refresh to trigger it (if refresh is 404, the chain breaks)
🚨Nginx Catch-All trap: a PHP CMS with rewrite routing everything → every path returns 200 with the same size = catch-all (not proof that a file exists!). Identify it by comparing three random paths; identical sizes = catch-all. Test truly live paths with a .php suffix (nginx `location ~ \.php$` passes directly to FastCGI; a nonexistent file returns a real 404).
🚨Broken Pure-FTPd puredb ≠ authentication bypass: "Unable to read the indexed puredb file" means the virtual-user database is broken; every username (including anonymous) triggers a 421 disconnect. PAM system-user authentication also passes through puredb first → all attempts fail. This is not an attack surface; skip it.
🚨Lateral movement across sites on one IP: reverse the IP (SecurityTrails/ThreatBook/VirusTotal) to find other domains on the same server → compromise a weaker site (an independent nginx configuration returning real 404s is enumerable) → centralized BT Panel management enables lateral movement to every site.
Fingerprint priority: as soon as httpx/wappalyzer identifies a framework version → switch immediately to `component-vuln-intel` online intelligence. Map the attack surface first, then Reason about the richest edge; do not dive headfirst into the first vulnerability you see.
🚨Detect wildcard-DNS CDNs: every subdomain (including random nonexistent names) resolves to the CDN = wildcard DNS (*.domain → CDN CNAME). Subdomain enumeration is then meaningless; every IP is a CDN node.
  Verification: dig random123456.target.com → if it also has an A record pointing to a known CDN IP = wildcard. Obtain the real origin IP through other channels (historical DNS/Certificate Transparency/other domains on the same server/email headers/SSRF)
🚨Identify BT Panel: a Cookie name that is a 32-character hex hash (such as 789d6d2de16419a1e8bbfea926c8996e) + 302 redirect to /login + nginx reverse proxy + an "入口校验失败" page = a random BT Panel security entry path
  Windows version: IIS+nginx coexist; panel on port 8888, possibly phpMyAdmin on port 888; /index.html may leak site configuration (reverse-proxy target URL/site name)
  Attack: brute-force the entry path (8 random alphanumeric characters) | historical CVEs (unauthenticated API key/phar deserialization) | reuse the panel's default FTP/MySQL accounts
🚨Nginx 404 differential fingerprinting: when nginx catch-all rewrites hide real paths, compare response sizes: a real nginx 404 (146B) = file exists on disk but the location block denies it; catch-all (large page) = route did not match. Batch-probe CMS indicator paths (/caches/configs/, /phpcms/modules/, /statics/, etc.); response-size differences identify the CMS type. See references/nginx-404-differential-fingerprinting.md
🚨Fast path for reversing UniApp/DCloud APKs: assets/apps/__UNI__*/www/app-service.js = all business logic (obfuscated JS); assets/zlsioh.dat = encrypted configuration (API domains); lib/arm64-v8a/libirzlemnr.so = decryption library. Deobfuscate JS with RC4+base64 (extract the a0G array+a0m decoder); search strings in the .so for random 16-character strings = possible keys, and for the `x数字.数字.数字.数字x` format = hardcoded IPs (usually DCloud HTTPDNS infrastructure, not the target API). See references/uniapp-dcloud-apk-reversing.md
```

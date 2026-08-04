---
name: proxy-tool-bootstrap
description: >-
  Self-sourced proxies+tool bootstrapping: SOCKS5/HTTP/Tor rerouting sequence, Python tool bootstrapping, generated dictionaries, and OOB infrastructure. Use when blocked by 403/429/WAF/timeout, missing tools, or needing OOB confirmation.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Self-Sourced Proxies + Tool Bootstrapping (reroute when blocked; write missing tools yourself)

```

🔴Proxy (on rejection/rate limiting/timeout → the first response is not retrying but rerouting. Not rerouting = giving up = violation of blackboard trigger 2 (see `pentest-blackboard`)):
  Execute in sequence (in order; proceed only after the previous step fails):
  ①Probe target region: terminal: curl -s "http://ip-api.com/json/{target IP}" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d['country'],d['countryCode'])"
  ②Obtain a regional SOCKS5 proxy: terminal: curl -s "https://api.proxyscrape.com/v2/?request=displayproxies&protocol=socks5&country={countryCode}&timeout=5000" | head -20
  ③Verify proxy reaches target: terminal: curl --socks5 {PROXY_IP:PORT} --connect-timeout 8 -s -o /dev/null -w "%{http_code}" {target URL}
  ④200 = usable; immediately retry the blocked request through this proxy; non-200 = switch to the next proxy and repeat ③
  ⑤All SOCKS5 failures → obtain HTTP proxies: terminal: curl -s "https://api.proxyscrape.com/v2/?request=displayproxies&protocol=http&country={countryCode}&timeout=5000" | head -20
  ⑥Verify HTTP proxy: terminal: curl --proxy http://{PROXY_IP:PORT} --connect-timeout 8 -s -o /dev/null -w "%{http_code}" {target URL}
  ⑦All proxies fail → Tor: terminal: curl --socks5 127.0.0.1:9050 --connect-timeout 15 {target URL}
  Add proxy parameters consistently: curl --socks5 / sqlmap --proxy=socks5://{P} / nmap --proxies socks5://{P} / nuclei -proxy socks5://{P} / ffuf -x socks5://{P}
  Rotation strategy: 429/403 → immediately switch to the next proxy; rotate proactively every 20 requests (avoid banning the new IP too) | Cloudflare → proxy pool+random 2-5s request intervals
  🚨HTTP proxy vs SOCKS5: an HTTP proxy inserts its own error page (502/cannot display page), making the target's actual response indistinguishable → probes must use SOCKS5 (--socks5)
    ProxyScrape SOCKS5: https://api.proxyscrape.com/v2/?request=displayproxies&protocol=socks5&country=CN,JP&timeout=5000
    Verify SOCKS5: curl --socks5 IP:PORT --connect-timeout 5 target | HTTP proxies are only for anonymity/rotation against confirmed-reachable targets
Tool bootstrapping (which X || implement in Python):
  no nmap → socket port scan | no ffuf → requests directory brute force | no sqlmap → manual payload checks | no hydra → requests brute force | no nuclei → requests against known payloads
  Simulate complex tools in Python: crawler requests+bs4 / encoding base64/hex / hashing hashlib / cryptography pycryptodome / sniffing scapy
Generate dictionaries: make variants from target domain/company name | extract keywords from web pages | combine username+year+special characters | service default credentials
OOB infrastructure (all blind vulnerabilities rely on it): use interactsh-client for an oast.fun domain | or VPS python3 -m http.server/nc to observe callbacks | ngrok/cloudflared tunnels
  → only an observed OOB callback (DNS query/HTTP request) counts as confirmation → write a Fact
```

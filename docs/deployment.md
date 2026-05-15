# Docker 部署

## 快速开始

从源码构建镜像：

```bash
docker build -t cyberstrikeai:local .
```

使用本地配置和数据目录启动：

```bash
docker run -d \
  --name cyberstrikeai \
  -p 7022:7022 \
  -p 8081:8081 \
  -v "$(pwd)/.docker-runtime:/app/runtime-config" \
  -v "$(pwd)/data:/app/data" \
  -v "$(pwd)/tmp:/app/tmp" \
  -v "$(pwd)/knowledge_base:/app/knowledge_base" \
  cyberstrikeai:local
```

使用 Compose：

```bash
docker compose up -d --build
```

仓库自带的 `docker-compose.yml` 会从当前检出的源码本地构建镜像，适合源码部署和二次开发。

默认 Docker 配置在 `7022` 端口启用 HTTPS（内存自签证书）并将同端口 HTTP 自动 308 跳转到 HTTPS。首次访问请打开 `https://127.0.0.1:7022/` 并信任自签证书；如需纯 HTTP，可在 `/app/runtime-config/config.yaml` 中关闭 `server.tls_enabled` 与 `server.tls_auto_self_sign` 后重启容器。

## GHCR 镜像

仓库会通过 GitHub Actions 自动发布镜像到 GHCR：

```bash
docker pull ghcr.io/ed1s0nz/cyberstrikeai:latest
```

如果只想运行官方镜像，可以把 `docker run` 示例里的 `cyberstrikeai:local` 替换成 GHCR 标签，或自行编写一个基于 `image:` 的 compose 文件。

## 持久化目录

- `/app/runtime-config/config.yaml`：Docker 运行态配置文件，首次启动时会由镜像内的 `config.docker.yaml` 模板自动生成
- `/app/data`：SQLite 数据库
- `/app/tmp`：大结果和临时输出
- `/app/knowledge_base`：知识库文件

容器内应用仍通过 `/app/config.yaml` 读取配置，但 Docker 镜像会将它链接到 `/app/runtime-config/config.yaml`，避免把仓库根的 `config.yaml` 当作可写运行态文件。

## 权限说明

镜像默认以 `root` 运行，优先保证工具可用性。大多数功能不需要 `--privileged`，但少数依赖原始套接字或高级网络探测的工具需要额外能力，例如：

```bash
docker run ... --cap-add NET_ADMIN --cap-add NET_RAW cyberstrikeai:local
```

`docker-compose.yml` 里已经预留了对应注释，按需取消即可。

## 预装工具说明

Docker 构建会安装内置工具定义实际调用的本地命令，包括：

- Go / Rust / Node 工具：`httpx`、`nuclei`、`subfinder`、`ffuf`、`gobuster`、`dalfox`、`amass`、`katana`、`rustscan`、`pwninit`、`spectral`
- 二进制和 APT 工具：`nmap`、`sqlmap`、`nikto`、`masscan`、`john`、`gdb`、`binwalk`、`steghide`、`radare2`（`r2`）、`foremost`、`tshark`、`tcpdump`、`trivy`、`kube-bench`、`arp-scan`
- Python / Ruby 工具：`checkov`、`volatility3`、`ROPgadget`、`ropper`、`smbmap`、`fierce`、`prowler`、`scout`、`kube-hunter`、`paramspider`、`responder`、`enum4linux-ng`、`one_gadget`、`wafw00f`、`wpscan`，以及 `requirements.txt` 中声明的依赖

在 `amd64` 上，必需工具命令缺失会导致镜像构建失败。`arm64` 构建会在上游发布兼容产物时使用相同安装路径；如存在特定架构例外，应明确记录受影响工具。

`rustscan`、`tcpdump`、`arp-scan`、`responder`、`tshark` 等原始数据包和网络工具，根据扫描模式可能需要 `NET_RAW`、`NET_ADMIN`、host networking 或 `--privileged`。`prowler`、`scout` 等云安全工具需要挂载凭据文件或通过环境变量提供凭据。`kube-hunter`、`kube-bench` 等 Kubernetes 工具，根据检查类型可能需要 kubeconfig、节点文件系统访问、宿主机命名空间或显式目标配置。

## 升级

容器部署不使用 `run.sh` 或 `upgrade.sh`。

源码 + Compose 部署：

```bash
git pull
docker compose up -d --build
```

GHCR 预构建镜像部署：

```bash
docker pull ghcr.io/ed1s0nz/cyberstrikeai:latest
docker rm -f cyberstrikeai
docker run -d --name cyberstrikeai -p 7022:7022 -p 8081:8081 -v "$(pwd)/.docker-runtime:/app/runtime-config" -v "$(pwd)/data:/app/data" -v "$(pwd)/tmp:/app/tmp" -v "$(pwd)/knowledge_base:/app/knowledge_base" ghcr.io/ed1s0nz/cyberstrikeai:latest
```

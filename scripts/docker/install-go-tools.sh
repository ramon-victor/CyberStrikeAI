#!/usr/bin/env bash

set -euo pipefail

DEST_DIR="${1:-/out/bin}"

mkdir -p "${DEST_DIR}"

export GOBIN="${DEST_DIR}"

go install github.com/projectdiscovery/httpx/cmd/httpx@v1.6.8
go install github.com/projectdiscovery/nuclei/v3/cmd/nuclei@v3.3.0
go install github.com/projectdiscovery/subfinder/v2/cmd/subfinder@v2.6.6
go install github.com/ffuf/ffuf/v2@v2.1.0
go install github.com/OJ/gobuster/v3@v3.8.1
go install github.com/hahwul/dalfox/v2@v2.12.0
go install github.com/projectdiscovery/katana/cmd/katana@v1.1.0
CGO_ENABLED=0 go install -v github.com/owasp-amass/amass/v5/cmd/amass@v5.0.1
go install github.com/tomnomnom/waybackurls@latest
go install github.com/lc/gau/v2/cmd/gau@latest
go install github.com/jaeles-project/jaeles@latest

terrascan_arch="x86_64"
case "${TARGETARCH:-amd64}" in
    arm64) terrascan_arch="arm64" ;;
esac
terrascan_ver="1.19.9"
curl -fsSL "https://github.com/tenable/terrascan/releases/download/v${terrascan_ver}/terrascan_${terrascan_ver}_Linux_${terrascan_arch}.tar.gz" -o /tmp/terrascan.tar.gz
tar -xzf /tmp/terrascan.tar.gz -C "${DEST_DIR}" terrascan
chmod +x "${DEST_DIR}/terrascan"
rm -f /tmp/terrascan.tar.gz

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

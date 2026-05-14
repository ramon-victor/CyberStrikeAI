#!/usr/bin/env bash

set -Eeuo pipefail

export DEBIAN_FRONTEND=noninteractive

log() {
    printf '[docker-tools] %s\n' "$*"
}

warn() {
    printf '[docker-tools][warn] %s\n' "$*" >&2
}

apt_install_optional() {
    local package="$1"

    if apt-cache show "${package}" >/dev/null 2>&1; then
        if ! apt-get install -y --no-install-recommends "${package}"; then
            warn "failed to install optional apt package: ${package}"
        fi
    else
        warn "apt package not available on this platform: ${package}"
    fi
}

pip_install_optional() {
    if ! python3 -m pip install --break-system-packages --no-cache-dir "$@"; then
        warn "failed to install optional Python packages: $*"
    fi
}

gem_install_optional() {
    if ! gem install --no-document "$@"; then
        warn "failed to install optional Ruby gems: $*"
    fi
}

create_venv() {
    local name="$1"
    local venv="/opt/cyberstrike/venvs/${name}"

    python3 -m venv "${venv}"
    "${venv}/bin/python" -m pip install --no-cache-dir --upgrade 'pip' 'setuptools>=66,<82' 'wheel'
}

install_venv_package() {
    local name="$1"
    local package="$2"
    local command_name="$3"
    local venv="/opt/cyberstrike/venvs/${name}"

    create_venv "${name}"
    "${venv}/bin/python" -m pip install --no-cache-dir "${package}"
    ln -sf "${venv}/bin/${command_name}" "/usr/local/bin/${command_name}"
}

log "installing base runtime packages"
apt-get update
echo "wireshark-common wireshark-common/install-setuid boolean false" | debconf-set-selections
apt-get install -y --no-install-recommends \
    bash \
    build-essential \
    ca-certificates \
    curl \
    dirmngr \
    dnsutils \
    file \
    git \
    gnupg \
    gpg \
    iputils-ping \
    jq \
    libio-socket-ssl-perl \
    libjson-perl \
    liblzma5 \
    libnet-ssleay-perl \
    libpcap0.8 \
    libssl3 \
    libxml-writer-perl \
    netcat-openbsd \
    nmap \
    patchelf \
    perl \
    python3 \
    python3-dev \
    python3-netifaces \
    python3-pip \
    python3-setuptools \
    python3-venv \
    ruby-full \
    smbclient \
    samba-common-bin \
    sqlite3 \
    tini \
    unzip \
    wget \
    xz-utils \
    foremost \
    tshark \
    tcpdump \
    arp-scan

log "installing Trivy from Aqua Security signed apt repository"
rm -f /usr/share/keyrings/trivy.gpg /etc/apt/sources.list.d/trivy.list
curl -fsSL https://aquasecurity.github.io/trivy-repo/deb/public.key | gpg --dearmor -o /usr/share/keyrings/trivy.gpg
printf 'deb [signed-by=/usr/share/keyrings/trivy.gpg] https://aquasecurity.github.io/trivy-repo/deb generic main\n' > /etc/apt/sources.list.d/trivy.list
apt-get update
apt-get install -y --no-install-recommends trivy

log "installing best-effort security packages from apt"
for package in \
    binwalk \
    dirb \
    dnsenum \
    gdb \
    hashcat \
    john \
    libimage-exiftool-perl \
    masscan \
    nikto \
    sqlmap \
    steghide
do
    apt_install_optional "${package}"
done

log "setting up /usr/share/wordlists symlinks"
mkdir -p /usr/share/wordlists
if [ -d /usr/share/dirb/wordlists ] && [ ! -e /usr/share/wordlists/dirb ]; then
    ln -s /usr/share/dirb/wordlists /usr/share/wordlists/dirb
fi

log "preinstalling pycryptodomex from source to avoid transient wheel digest mismatches"
python3 -m pip install --break-system-packages --no-cache-dir --no-binary pycryptodomex pycryptodomex

log "installing Python dependencies used by bundled tools"
python3 -m pip install --break-system-packages --no-cache-dir -r requirements.txt

log "installing required Python CLI tools"
python3 -m pip install --break-system-packages --no-cache-dir \
    ROPgadget \
    ropper \
    volatility3 \
    smbmap \
    fierce
printf '#!/usr/bin/env bash\nexec vol "$@"\n' > /usr/local/bin/volatility3
chmod +x /usr/local/bin/volatility3

log "installing extra best-effort Python tooling"
pip_install_optional \
    checkov \
    wafw00f

log "installing Prowler in an isolated virtualenv"
install_venv_package prowler prowler prowler

log "installing Scout Suite in an isolated virtualenv"
install_venv_package scout-suite ScoutSuite scout

log "installing kube-hunter from GitHub in an isolated virtualenv"
git clone --depth=1 https://github.com/aquasecurity/kube-hunter.git /opt/kube-hunter
create_venv kube-hunter
/opt/cyberstrike/venvs/kube-hunter/bin/python -m pip install --no-cache-dir --no-build-isolation /opt/kube-hunter
ln -sf /opt/cyberstrike/venvs/kube-hunter/bin/kube-hunter /usr/local/bin/kube-hunter

log "installing ParamSpider from GitHub in an isolated virtualenv"
git clone --depth=1 https://github.com/devanshbatham/ParamSpider.git /opt/ParamSpider
create_venv paramspider
/opt/cyberstrike/venvs/paramspider/bin/python -m pip install --no-cache-dir /opt/ParamSpider
ln -sf /opt/cyberstrike/venvs/paramspider/bin/paramspider /usr/local/bin/paramspider

log "installing Responder from GitHub"
git clone --depth=1 https://github.com/lgandx/Responder.git /opt/Responder
create_venv responder
if [ -f /opt/Responder/requirements.txt ]; then
    /opt/cyberstrike/venvs/responder/bin/python -m pip install --no-cache-dir -r /opt/Responder/requirements.txt
fi
printf '#!/usr/bin/env bash\nexec /opt/cyberstrike/venvs/responder/bin/python /opt/Responder/Responder.py "$@"\n' > /usr/local/bin/responder
chmod +x /usr/local/bin/responder

log "installing enum4linux-ng from GitHub"
git clone --depth=1 https://github.com/cddmp/enum4linux-ng.git /opt/enum4linux-ng
create_venv enum4linux-ng
/opt/cyberstrike/venvs/enum4linux-ng/bin/python -m pip install --no-cache-dir -r /opt/enum4linux-ng/requirements.txt
printf '#!/usr/bin/env bash\nexec /opt/cyberstrike/venvs/enum4linux-ng/bin/python /opt/enum4linux-ng/enum4linux-ng.py "$@"\n' > /usr/local/bin/enum4linux-ng
chmod +x /usr/local/bin/enum4linux-ng

log "installing LinPEAS"
curl -fsSL https://github.com/peass-ng/PEASS-ng/releases/latest/download/linpeas.sh -o /usr/local/bin/linpeas.sh
chmod +x /usr/local/bin/linpeas.sh

log "installing kube-bench release package"
case "${TARGETARCH:-amd64}" in
    amd64) kube_bench_arch="amd64" ;;
    arm64) kube_bench_arch="arm64" ;;
    *)
        printf 'unsupported TARGETARCH for kube-bench: %s\n' "${TARGETARCH:-unset}" >&2
        exit 1
        ;;
esac
kube_bench_url="$(KUBE_BENCH_ARCH="${kube_bench_arch}" python3 - <<'PY'
import json
import os
import sys
import urllib.request

arch = os.environ["KUBE_BENCH_ARCH"]
request = urllib.request.Request(
    "https://api.github.com/repos/aquasecurity/kube-bench/releases/latest",
    headers={"Accept": "application/vnd.github+json", "User-Agent": "CyberStrikeAI-Docker-build"},
)
with urllib.request.urlopen(request, timeout=60) as response:
    release = json.load(response)
for asset in release.get("assets", []):
    name = asset.get("name", "")
    if name.endswith(f"linux_{arch}.deb"):
        print(asset["browser_download_url"])
        sys.exit(0)
print(f"no kube-bench linux_{arch}.deb asset found", file=sys.stderr)
sys.exit(1)
PY
)"
curl -fsSL "${kube_bench_url}" -o /tmp/kube-bench.deb
apt-get install -y --no-install-recommends /tmp/kube-bench.deb
rm -f /tmp/kube-bench.deb
test -d /etc/kube-bench || test -d /opt/kube-bench/cfg

log "installing one_gadget Ruby gem"
gem install --no-document one_gadget

log "installing dirsearch from GitHub (PyPI version is broken on Python 3.11+)"
if git clone --depth=1 https://github.com/maurosoria/dirsearch.git /opt/dirsearch 2>/dev/null; then
    python3 -m pip install --break-system-packages --no-cache-dir -r /opt/dirsearch/requirements.txt || true
    printf '#!/usr/bin/env bash\nexec python3 /opt/dirsearch/dirsearch.py "$@"\n' > /usr/local/bin/dirsearch
    chmod +x /usr/local/bin/dirsearch
    log "dirsearch installed from GitHub"
else
    warn "failed to install dirsearch from GitHub"
fi

log "installing extra best-effort Ruby tooling"
gem_install_optional wpscan

log "installing nikto from GitHub (ensures latest version with all dependencies)"
if ! command -v nikto >/dev/null 2>&1; then
    if git clone --depth=1 https://github.com/sullo/nikto.git /opt/nikto 2>/dev/null; then
        ln -sf /opt/nikto/program/nikto.pl /usr/local/bin/nikto
        chmod +x /opt/nikto/program/nikto.pl
        log "nikto installed from GitHub"
    else
        warn "failed to install nikto from GitHub"
    fi
fi

log "installing SecLists wordlists"
mkdir -p /usr/share/wordlists
if git clone --depth=1 https://github.com/danielmiessler/SecLists.git /usr/share/wordlists/seclists 2>/dev/null; then
    log "SecLists installed to /usr/share/wordlists/seclists"
else
    warn "failed to install SecLists wordlists"
fi

log "verifying required runtime-installed tool commands"
for command in responder linpeas.sh one_gadget ROPgadget ropper volatility3 foremost tshark tcpdump trivy prowler scout kube-hunter kube-bench smbmap enum4linux-ng arp-scan fierce paramspider; do
    command -v "${command}" >/dev/null || { printf 'missing required tool: %s\n' "${command}" >&2; exit 1; }
done

apt-get clean
rm -rf /var/lib/apt/lists/*

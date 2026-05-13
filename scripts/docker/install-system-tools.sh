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

log "installing base runtime packages"
apt-get update
apt-get install -y --no-install-recommends \
    bash \
    build-essential \
    ca-certificates \
    curl \
    dnsutils \
    file \
    git \
    iputils-ping \
    jq \
    netcat-openbsd \
    perl \
    libjson-perl \
    libnet-ssleay-perl \
    libio-socket-ssl-perl \
    libxml-writer-perl \
    python3 \
    python3-pip \
    python3-venv \
    ruby-full \
    sqlite3 \
    tini \
    unzip \
    wget \
    xz-utils

log "installing best-effort security packages from apt"
for package in \
    amass \
    binwalk \
    dirb \
    dnsenum \
    gdb \
    hashcat \
    john \
    libimage-exiftool-perl \
    masscan \
    nikto \
    nmap \
    radare2 \
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

log "installing Python dependencies used by bundled tools"
python3 -m pip install --break-system-packages --no-cache-dir -r requirements.txt

log "installing extra best-effort Python tooling"
pip_install_optional \
    checkov \
    volatility3 \
    wafw00f

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

apt-get clean
rm -rf /var/lib/apt/lists/*

# syntax=docker/dockerfile:1.7

FROM golang:1.24-bookworm AS app-builder

WORKDIR /src

RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential ca-certificates pkg-config \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o /out/cyberstrike-ai ./cmd/server/main.go

FROM golang:1.26-bookworm AS tools-builder

ARG TARGETARCH

WORKDIR /src

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl git \
    && rm -rf /var/lib/apt/lists/*

COPY scripts/docker/install-go-tools.sh /usr/local/bin/install-go-tools.sh

RUN chmod +x /usr/local/bin/install-go-tools.sh \
    && /usr/local/bin/install-go-tools.sh /out/bin


FROM rust:bookworm AS rust-tools-builder

WORKDIR /src

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates liblzma-dev libssl-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*

RUN cargo install rustscan --locked \
    && cargo install pwninit --locked \
    && cargo install x8 \
    && mkdir -p /out/bin \
    && cp /usr/local/cargo/bin/rustscan /usr/local/cargo/bin/pwninit /usr/local/cargo/bin/x8 /out/bin/

FROM debian:bookworm AS radare2-builder

WORKDIR /src

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git build-essential pkg-config \
    && rm -rf /var/lib/apt/lists/*

RUN git clone --depth=1 https://github.com/radareorg/radare2.git /src/radare2 \
    && cd /src/radare2 \
    && ./configure --prefix=/opt/radare2 \
    && make -j"$(nproc)" \
    && make install

FROM node:22-bookworm-slim AS node-tools-builder

RUN npm install -g @stoplight/spectral-cli tslib \
    && rm -f /usr/local/bin/spectral \
    && printf '#!/usr/bin/env bash\nexec node /usr/local/lib/node_modules/@stoplight/spectral-cli/dist/index.js "$@"\n' > /usr/local/bin/spectral \
    && chmod +x /usr/local/bin/spectral

FROM debian:bookworm-slim AS runtime

ENV APP_HOME=/app \
    PATH=/opt/cyberstrike/bin:/opt/radare2/bin:$PATH \
    LD_LIBRARY_PATH=/opt/radare2/lib \
    NODE_PATH=/usr/local/lib/node_modules \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PYTHONUNBUFFERED=1

WORKDIR ${APP_HOME}

ARG TARGETARCH

COPY requirements.txt ./requirements.txt
COPY scripts/docker/install-system-tools.sh /usr/local/bin/install-system-tools.sh
COPY scripts/docker/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

RUN chmod +x /usr/local/bin/install-system-tools.sh /usr/local/bin/docker-entrypoint.sh \
    && /usr/local/bin/install-system-tools.sh \
    && rm -f /usr/local/bin/install-system-tools.sh

COPY --from=app-builder /out/cyberstrike-ai ./cyberstrike-ai
COPY --from=tools-builder /out/bin/ /opt/cyberstrike/bin/
COPY --from=rust-tools-builder /out/bin/ /opt/cyberstrike/bin/
COPY --from=radare2-builder /opt/radare2/ /opt/radare2/
COPY --from=node-tools-builder /usr/local/bin/node /usr/local/bin/node
COPY --from=node-tools-builder /usr/local/bin/spectral /usr/local/bin/spectral
COPY --from=node-tools-builder /usr/local/lib/node_modules/ /usr/local/lib/node_modules/

RUN for command in rustscan amass responder linpeas.sh one_gadget r2 ROPgadget ropper pwninit x8 volatility3 foremost tshark tcpdump trivy prowler scout kube-hunter kube-bench smbmap enum4linux-ng arp-scan fierce paramspider katana spectral; do \
        command -v "${command}" >/dev/null || { echo "missing required tool: ${command}" >&2; exit 1; }; \
    done

COPY web ./web
COPY tools ./tools
COPY roles ./roles
COPY skills ./skills
COPY agents ./agents
COPY knowledge_base ./knowledge_base
COPY config.docker.yaml ./config.example.yaml

RUN mkdir -p runtime-config data tmp \
    && ln -s /app/runtime-config/config.yaml /app/config.yaml

EXPOSE 7022 8081

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=5 \
    CMD curl -kfsS https://127.0.0.1:7022/ >/dev/null || exit 1

ENTRYPOINT ["tini", "--", "/usr/local/bin/docker-entrypoint.sh"]
CMD ["./cyberstrike-ai"]

ARG UBUNTU_VERSION=26.04
ARG GO_VERSION=1.26.5
ARG GO_LINUX_AMD64_SHA256=5c2c3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393f053
ARG BUILD_GOMAXPROCS=2

FROM ubuntu:${UBUNTU_VERSION} AS go-base
ARG GO_VERSION
ARG GO_LINUX_AMD64_SHA256
ARG BUILD_GOMAXPROCS
ENV DEBIAN_FRONTEND=noninteractive
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        bash \
        ca-certificates \
        curl \
        file \
        git; \
    curl -fsSLo /tmp/go.tgz "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"; \
    echo "${GO_LINUX_AMD64_SHA256}  /tmp/go.tgz" | sha256sum -c -; \
    tar -C /usr/local -xzf /tmp/go.tgz; \
    rm -rf /tmp/go.tgz /var/lib/apt/lists/*
ENV PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    GOPATH=/go \
    GO111MODULE=off \
    GOFLAGS=-buildvcs=false \
    GOMAXPROCS=${BUILD_GOMAXPROCS}

FROM go-base AS etcd-builder
ARG ETCD_GIT_SHA=fd17c91
WORKDIR /src
COPY . .
RUN set -eux; \
    GIT_SHA="${ETCD_GIT_SHA}" ./build; \
    file bin/etcd bin/etcdctl; \
    bin/etcd --version; \
    bin/etcdctl --version

FROM go-base AS giddyup-builder
ARG GIDDYUP_REPO=https://github.com/rancher/giddyup.git
ARG GIDDYUP_COMMIT=1e5fefffc106a911df212eeed1457b4c85df11a4
RUN set -eux; \
    git clone "${GIDDYUP_REPO}" /src/giddyup; \
    cd /src/giddyup; \
    git checkout "${GIDDYUP_COMMIT}"; \
    mkdir -p /go/src/github.com/rancher; \
    ln -s /src/giddyup /go/src/github.com/rancher/giddyup; \
    cd /go/src/github.com/rancher/giddyup; \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/giddyup .; \
    /out/giddyup --help >/dev/null

FROM go-base AS etcdwrapper-builder
WORKDIR /src
COPY package/etcdwrapper/ .
RUN set -eux; \
    test -z "$(gofmt -l .)"; \
    go test ./...; \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/etcdwrapper .; \
    /out/etcdwrapper --help >/dev/null

FROM ubuntu:${UBUNTU_VERSION}
LABEL org.opencontainers.image.source="https://github.com/PastureStack/etcd-compat" \
      org.opencontainers.image.title="pasturestack-etcd-compat" \
      org.opencontainers.image.description="PastureStack etcd 2.3.7 compatibility runtime" \
      org.opencontainers.image.version="v2.3.7-pasturestack.2" \
      org.opencontainers.image.licenses="Apache-2.0"
ENV DEBIAN_FRONTEND=noninteractive \
    PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/opt/pasturestack
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        bash \
        bc \
        ca-certificates \
        curl \
        jq \
        procps \
        unzip \
        wget; \
    rm -rf /var/lib/apt/lists/*; \
    rm -f /usr/bin/pebble; \
    mkdir -p /opt/pasturestack /pdata /data /data-backup /etc/etcd/ssl /licenses
COPY --from=etcd-builder /src/bin/etcd /src/bin/etcdctl /opt/pasturestack/
COPY --from=giddyup-builder /out/giddyup /opt/pasturestack/giddyup
COPY --from=etcdwrapper-builder /out/etcdwrapper /opt/pasturestack/etcdwrapper
COPY package/platform-compat/run.sh package/platform-compat/delete package/platform-compat/disaster /opt/pasturestack/
COPY package/update-platform-ca /usr/bin/update-platform-ca
COPY LICENSE ORIGIN.md SECURITY.md COMPATIBILITY.md /licenses/
RUN set -eux; \
    chmod 0755 /opt/pasturestack/etcd /opt/pasturestack/etcdctl /opt/pasturestack/giddyup /opt/pasturestack/etcdwrapper \
        /opt/pasturestack/run.sh /opt/pasturestack/delete /opt/pasturestack/disaster /usr/bin/update-platform-ca; \
    ln -sf /opt/pasturestack/etcd /usr/local/bin/etcd; \
    ln -sf /opt/pasturestack/etcdctl /usr/local/bin/etcdctl; \
    ln -sf /opt/pasturestack/giddyup /usr/local/bin/giddyup; \
    ln -sf /opt/pasturestack/etcdwrapper /usr/local/bin/etcdwrapper; \
    etcd --version; \
    etcdctl --version; \
    giddyup --help >/dev/null; \
    etcdwrapper --help >/dev/null
WORKDIR /pdata
ENTRYPOINT ["/opt/pasturestack/run.sh"]
CMD ["node"]

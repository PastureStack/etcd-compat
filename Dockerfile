# syntax=docker/dockerfile:1.7
ARG UBUNTU_IMAGE=ubuntu:26.04@sha256:678c6550cc43645e08669028bc177f50be4e7c5b8cca677067b1914d4afc7a03

FROM ${UBUNTU_IMAGE} AS ca-trust
ADD --checksum=sha256:6077d27c6b6f8b23590cb01ff877ed8c804a67a5442cc32b5a33da10d2bd0e90 \
    https://snapshot.ubuntu.com/ubuntu/20260808T000000Z/pool/main/c/ca-certificates/ca-certificates_20260601~26.04.1_all.deb \
    /tmp/ca-certificates.deb
ADD --checksum=sha256:c1f53878bdada693da7fb64a28c06b7dd65a43b8452e6fcad670c0d09c77f293 \
    https://snapshot.ubuntu.com/ubuntu/20260808T000000Z/pool/main/o/openssl/openssl_3.5.5-1ubuntu3.3_amd64.deb \
    /tmp/openssl.deb
RUN set -eux; \
    dpkg -i /tmp/openssl.deb /tmp/ca-certificates.deb; \
    update-ca-certificates --fresh; \
    test -s /etc/ssl/certs/ca-certificates.crt; \
    test "$(openssl version | awk '{print $2}')" = 3.5.5; \
    rm -f /tmp/ca-certificates.deb /tmp/openssl.deb

FROM ${UBUNTU_IMAGE} AS go-base
ARG GO_VERSION=1.26.5
ARG GO_LINUX_AMD64_SHA256=5c2c3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393f053
ARG BUILD_GOMAXPROCS=2
COPY package/ubuntu-apt.lock /tmp/ubuntu-apt.lock
COPY --from=ca-trust /etc/ca-certificates.conf /etc/ca-certificates.conf
COPY --from=ca-trust /etc/ssl/certs /etc/ssl/certs
COPY --from=ca-trust /usr/share/ca-certificates /usr/share/ca-certificates
ENV DEBIAN_FRONTEND=noninteractive
RUN set -eux; \
    . /tmp/ubuntu-apt.lock; \
    printf 'Types: deb\nURIs: https://snapshot.ubuntu.com/ubuntu/%s/\nSuites: %s %s-updates %s-backports %s-security\nComponents: main universe restricted multiverse\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n' \
        "${UBUNTU_APT_SNAPSHOT}" "${UBUNTU_APT_SUITE}" "${UBUNTU_APT_SUITE}" \
        "${UBUNTU_APT_SUITE}" "${UBUNTU_APT_SUITE}" \
        > /etc/apt/sources.list.d/ubuntu.sources; \
    printf 'Acquire::https::CaInfo "/etc/ssl/certs/ca-certificates.crt";\n' \
        > /etc/apt/apt.conf.d/99pasturestack-ca; \
    rm -f /etc/apt/sources.list; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        bash="${UBUNTU_APT_BASH_VERSION}" \
        build-essential="${UBUNTU_APT_BUILD_ESSENTIAL_VERSION}" \
        ca-certificates="${UBUNTU_APT_CA_CERTIFICATES_VERSION}" \
        curl="${UBUNTU_APT_CURL_VERSION}" \
        file="${UBUNTU_APT_FILE_VERSION}" \
        gcc="${UBUNTU_APT_GCC_VERSION}" \
        libc6-dev="${UBUNTU_APT_LIBC6_DEV_VERSION}" \
        openssl="${UBUNTU_APT_OPENSSL_VERSION}" \
        patch="${UBUNTU_APT_PATCH_VERSION}" \
        tar="${UBUNTU_APT_TAR_VERSION}"; \
    rm -rf /var/lib/apt/lists/*; \
    curl --fail --silent --show-error --location --retry 5 --retry-all-errors \
        --output /tmp/go.tgz "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"; \
    echo "${GO_LINUX_AMD64_SHA256}  /tmp/go.tgz" | sha256sum -c -; \
    tar -C /usr/local -xzf /tmp/go.tgz; \
    rm -f /tmp/go.tgz; \
    test "$(/usr/local/go/bin/go env GOVERSION)" = "go${GO_VERSION}"
ENV PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    GOPATH=/go \
    GO111MODULE=off \
    GOFLAGS="-buildvcs=false -trimpath" \
    GOTELEMETRY=off \
    GOMAXPROCS=${BUILD_GOMAXPROCS}

FROM go-base AS etcd-builder
ARG SOURCE_REVISION=fd17c9101d94703f6f4c3d8d6cfb72b62b894cd7
WORKDIR /src
COPY . .
RUN set -eux; \
    printf '%s\n' "${SOURCE_REVISION}" | grep -Eq '^[0-9a-f]{40}$'; \
    GIT_SHA="${SOURCE_REVISION}" ./build; \
    cp bin/etcd /tmp/etcd.first; \
    cp bin/etcdctl /tmp/etcdctl.first; \
    rm -rf bin; \
    GIT_SHA="${SOURCE_REVISION}" ./build; \
    cmp /tmp/etcd.first bin/etcd; \
    cmp /tmp/etcdctl.first bin/etcdctl; \
    rm -f /tmp/etcd.first /tmp/etcdctl.first; \
    file bin/etcd bin/etcdctl | grep -F 'statically linked'; \
    bin/etcd --version; \
    bin/etcdctl --version; \
    mkdir -p /out/etcd-third-party-licenses; \
    find Godeps/_workspace -type f \
        \( -iname 'LICENSE*' -o -iname 'NOTICE*' -o -iname 'COPYING*' \) \
        -print | LC_ALL=C sort | while IFS= read -r legal_file; do \
            install -D -m 0644 "${legal_file}" "/out/etcd-third-party-licenses/${legal_file}"; \
        done; \
    cd /out; \
    find etcd-third-party-licenses -type f -print0 | LC_ALL=C sort -z | \
        xargs -0 sha256sum > ETCD-THIRD-PARTY-LICENSE-SHA256.txt

FROM go-base AS giddyup-builder
ARG GIDDYUP_COMMIT=1e5fefffc106a911df212eeed1457b4c85df11a4
ARG GIDDYUP_ARCHIVE_SHA256=394e923a30d278075c3d980ab5d4ce87f0bfce68c4a42c00de40195bb902d5d4
COPY package/giddyup-go1.26.patch /tmp/giddyup-go1.26.patch
RUN set -eux; \
    curl --fail --silent --show-error --location --retry 5 --retry-all-errors \
        --output /tmp/giddyup.tar.gz \
        "https://codeload.github.com/rancher/giddyup/tar.gz/${GIDDYUP_COMMIT}"; \
    echo "${GIDDYUP_ARCHIVE_SHA256}  /tmp/giddyup.tar.gz" | sha256sum -c -; \
    mkdir -p /src/giddyup; \
    tar -xzf /tmp/giddyup.tar.gz -C /src/giddyup --strip-components=1; \
    rm -f /tmp/giddyup.tar.gz; \
    cd /src/giddyup; \
    patch --batch --forward --strip=1 < /tmp/giddyup-go1.26.patch; \
    test -z "$(gofmt -l election/proxy.go)"; \
    mkdir -p /go/src/github.com/rancher; \
    ln -s /src/giddyup /go/src/github.com/rancher/giddyup; \
    cd /go/src/github.com/rancher/giddyup; \
    go test -race ./...; \
    CGO_ENABLED=0 go build -ldflags='-s -w -buildid=' -o /out/giddyup.first .; \
    CGO_ENABLED=0 go build -ldflags='-s -w -buildid=' -o /out/giddyup.second .; \
    cmp /out/giddyup.first /out/giddyup.second; \
    mv /out/giddyup.first /out/giddyup; \
    rm -f /out/giddyup.second; \
    /out/giddyup --help >/dev/null; \
    mkdir -p /out/giddyup-third-party-licenses; \
    find /src/giddyup -type f \
        \( -iname 'LICENSE*' -o -iname 'NOTICE*' -o -iname 'COPYING*' \) \
        -print | LC_ALL=C sort | while IFS= read -r legal_file; do \
            relative_path=${legal_file#/src/giddyup/}; \
            install -D -m 0644 "${legal_file}" "/out/giddyup-third-party-licenses/${relative_path}"; \
        done; \
    cd /out; \
    find giddyup-third-party-licenses -type f -print0 | LC_ALL=C sort -z | \
        xargs -0 sha256sum > GIDDYUP-THIRD-PARTY-LICENSE-SHA256.txt; \
    printf 'source=https://github.com/rancher/giddyup\ncommit=%s\narchive_sha256=%s\n' \
        "${GIDDYUP_COMMIT}" "${GIDDYUP_ARCHIVE_SHA256}" > /out/GIDDYUP-ORIGIN.txt

FROM go-base AS etcdwrapper-builder
WORKDIR /src
COPY package/etcdwrapper/ .
RUN set -eux; \
    test -z "$(gofmt -l .)"; \
    go test -race ./...; \
    CGO_ENABLED=0 go build -ldflags='-s -w -buildid=' -o /out/etcdwrapper.first .; \
    CGO_ENABLED=0 go build -ldflags='-s -w -buildid=' -o /out/etcdwrapper.second .; \
    cmp /out/etcdwrapper.first /out/etcdwrapper.second; \
    mv /out/etcdwrapper.first /out/etcdwrapper; \
    rm -f /out/etcdwrapper.second; \
    /out/etcdwrapper --help >/dev/null

FROM ${UBUNTU_IMAGE}
ARG IMAGE_VERSION=2.3.8
ARG SOURCE_REVISION=fd17c9101d94703f6f4c3d8d6cfb72b62b894cd7
COPY package/ubuntu-apt.lock /tmp/ubuntu-apt.lock
COPY --from=ca-trust /etc/ca-certificates.conf /etc/ca-certificates.conf
COPY --from=ca-trust /etc/ssl/certs /etc/ssl/certs
COPY --from=ca-trust /usr/share/ca-certificates /usr/share/ca-certificates
ENV DEBIAN_FRONTEND=noninteractive \
    PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/opt/pasturestack
RUN set -eux; \
    . /tmp/ubuntu-apt.lock; \
    printf 'Types: deb\nURIs: https://snapshot.ubuntu.com/ubuntu/%s/\nSuites: %s %s-updates %s-backports %s-security\nComponents: main universe restricted multiverse\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n' \
        "${UBUNTU_APT_SNAPSHOT}" "${UBUNTU_APT_SUITE}" "${UBUNTU_APT_SUITE}" \
        "${UBUNTU_APT_SUITE}" "${UBUNTU_APT_SUITE}" \
        > /etc/apt/sources.list.d/ubuntu.sources; \
    printf 'Acquire::https::CaInfo "/etc/ssl/certs/ca-certificates.crt";\n' \
        > /etc/apt/apt.conf.d/99pasturestack-ca; \
    rm -f /etc/apt/sources.list; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        bash="${UBUNTU_APT_BASH_VERSION}" \
        bc="${UBUNTU_APT_BC_VERSION}" \
        ca-certificates="${UBUNTU_APT_CA_CERTIFICATES_VERSION}" \
        curl="${UBUNTU_APT_CURL_VERSION}" \
        jq="${UBUNTU_APT_JQ_VERSION}" \
        openssl="${UBUNTU_APT_OPENSSL_VERSION}" \
        unzip="${UBUNTU_APT_UNZIP_VERSION}" \
        wget="${UBUNTU_APT_WGET_VERSION}"; \
    rm -rf /var/lib/apt/lists/* /usr/bin/pebble; \
    mkdir -p /opt/pasturestack /pdata /data /data-backup /etc/etcd/ssl /licenses; \
    { printf 'snapshot\t%s\n' "${UBUNTU_APT_SNAPSHOT}"; \
      dpkg-query -W -f='${binary:Package}\t${Version}\n'; } | \
        LC_ALL=C sort > /licenses/ETCD-COMPAT-UBUNTU-APT-PACKAGES.tsv; \
    rm -f /tmp/ubuntu-apt.lock
LABEL org.opencontainers.image.source="https://github.com/PastureStack/etcd-compat" \
      org.opencontainers.image.title="pasturestack-etcd-compat" \
      org.opencontainers.image.description="PastureStack etcd 2.3 compatibility runtime" \
      org.opencontainers.image.version="${IMAGE_VERSION}" \
      org.opencontainers.image.revision="${SOURCE_REVISION}" \
      org.opencontainers.image.licenses="Apache-2.0" \
      io.pasturestack.upstream.version="2.3.7" \
      io.pasturestack.ubuntu.snapshot="20260808T000000Z" \
      io.pasturestack.giddyup.revision="1e5fefffc106a911df212eeed1457b4c85df11a4"
COPY --from=etcd-builder /src/bin/etcd /src/bin/etcdctl /opt/pasturestack/
COPY --from=giddyup-builder /out/giddyup /opt/pasturestack/giddyup
COPY --from=etcdwrapper-builder /out/etcdwrapper /opt/pasturestack/etcdwrapper
COPY package/platform-compat/run.sh package/platform-compat/delete package/platform-compat/disaster /opt/pasturestack/
COPY package/update-platform-ca /usr/bin/update-platform-ca
COPY LICENSE NOTICE ORIGIN.md SECURITY.md COMPATIBILITY.md /licenses/
COPY --from=etcd-builder /out/etcd-third-party-licenses /licenses/etcd-third-party
COPY --from=etcd-builder /out/ETCD-THIRD-PARTY-LICENSE-SHA256.txt /licenses/
COPY --from=giddyup-builder /out/giddyup-third-party-licenses /licenses/giddyup-third-party
COPY --from=giddyup-builder /out/GIDDYUP-THIRD-PARTY-LICENSE-SHA256.txt /out/GIDDYUP-ORIGIN.txt /licenses/
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

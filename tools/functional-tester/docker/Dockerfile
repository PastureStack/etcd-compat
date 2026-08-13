FROM ubuntu:26.04
ENV DEBIAN_FRONTEND=noninteractive
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends iptables sudo; \
    rm -rf /var/lib/apt/lists/*
ADD bin/etcd-agent /
ADD bin/etcd /
ADD bin/etcd-tester /
RUN mkdir /failure_archive
CMD ["./etcd-agent", "-etcd-path", "./etcd"]

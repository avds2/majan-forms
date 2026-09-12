FROM debian:13-slim

ENV container=docker
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update -qq \
    && apt-get install -qq -y -o Dpkg::Use-Pty=0 --no-install-recommends \
        adduser ca-certificates curl dbus postgresql postgresql-client systemd systemd-sysv \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]

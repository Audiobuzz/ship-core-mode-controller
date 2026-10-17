#!/bin/sh
# Runs at image build time, after docker-in-docker, so dockerd starts with the
# nft backend on first container start. Without it, hosts whose kernels lack
# the legacy nat table (seen on 7.0) fail with:
#   iptables v1.8.10 (legacy): can't initialize iptables table `nat'
set -e
update-alternatives --set iptables /usr/sbin/iptables-nft
update-alternatives --set ip6tables /usr/sbin/ip6tables-nft

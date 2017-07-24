#!/bin/sh

set -xe

name=${1:-"qnib/docker-plugin-metrics-opentsdb"}
docker build -f Dockerfile.pluginbuild -t "$name" .

id=$(docker create "$name")

rm -rf rootfs
mkdir -p rootfs
docker export "$id" | tar -xvf - -C rootfs
docker rm "$id"

rm -rf rootfs/proc rootfs/sys rootfs/go rootfs/etc rootfs/dev
docker plugin rm "$name"
docker plugin create "$name" .

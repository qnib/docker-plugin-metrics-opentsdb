#!/bin/sh

set -x

name=${1:-"qnib/docker-metrics-plugin-opentsdb"}
docker build -f Dockerfile.pluginbuild -t "$name" .

id=$(docker create "$name")

rm -rf rootfs
mkdir -p rootfs
docker export "$id" | tar -zxvf - -C rootfs
docker rm "$id"

rm -rf rootfs/proc rootfs/sys rootfs/go rootfs/etc rootfs/dev
docker plugin rm "$name"
docker plugin create "$name" .

#!/bin/sh

mkdir /run
mount -t tmpfs -o size=128m -o mode=755 tmpfs /run
mkdir /tmpfs
mount -t tmpfs -o size=128m tmpfs /tmpfs
wget -O /tmpfs/rootfs.squashfs http://192.168.1.12/rootfs.squashfs
mkdir /rootfs
mount -t squashfs -o loop /tmpfs/rootfs.squashfs /rootfs
mount -t devtmpfs -o mode=755 -o rw,nosuid udev /rootfs/dev
mount -t proc -o nodev,noexec,nosuid proc /rootfs/proc
mount -t sysfs -o nodev,noexec,nosuid sysfs /rootfs/sys
cd /rootfs
exec switch_root . /usr/lib/systemd/systemd --user --show-status --log-target=console --log-level=debug --log-color

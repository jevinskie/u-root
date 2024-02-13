// Copyright 2018 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"log"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/u-root/u-root/pkg/cmdline"
	"github.com/u-root/u-root/pkg/libinit"
	"github.com/u-root/u-root/pkg/uflag"
	"github.com/u-root/u-root/pkg/ulog"
)

func quiet() {
	if !*verbose {
		// Only messages more severe than "notice" are printed.
		if err := ulog.KernelLog.SetConsoleLogLevel(ulog.KLogDebug); err != nil {
			log.Printf("Could not set log level: %v", err)
		}
	}
}

func isSystemdEnabled() bool {
	initFlags := cmdline.GetInitFlagMap()

	systemd, present := initFlags["systemd"]
	if present {
		systemdEnabled, boolErr := strconv.ParseBool(systemd)
		if boolErr != nil {
			log.Printf("Failed parsing systemd '%s' error: %v", systemd, boolErr)
			return false
		}
		return systemdEnabled
	}
	return false
}

func isRootfsNetbootEnabled() bool {
	initFlags := cmdline.GetInitFlagMap()

	rootfsNetboot, present := initFlags["rootfs_netboot"]
	if present {
		rootfsNetbootEnabled, boolErr := strconv.ParseBool(rootfsNetboot)
		if boolErr != nil {
			log.Printf("Failed parsing rootfs_netboot '%s' error: %v", rootfsNetboot, boolErr)
			return false
		}
		return rootfsNetbootEnabled
	}
	return false
}

type RootfsNetbootInitPathError struct {
	What string
}

func (e RootfsNetbootInitPathError) Error() string {
	return e.What
}

func rootfsNetbootInitPath() (rootfsNetbootInitPath string, err error) {
	initFlags := cmdline.GetInitFlagMap()

	if !isRootfsNetbootEnabled() {
		log.Println("checking uroot.initflags for rootfs_netboot_init_path but rootfs_netboot=1 is not set!")
	}

	rootfsNetbootInitPath, present := initFlags["rootfs_netboot_init_path"]
	if present {
		return rootfsNetbootInitPath, nil
	}
	return "", RootfsNetbootInitPathError{"rootfs_netboot_init_path not present in uroot.initflags"}
}

func osInitGo() *initCmds {
	// Backwards compatibility for the transition from uroot.nohwrng to
	// UROOT_NOHWRNG=1 on kernel commandline.
	if cmdline.ContainsFlag("uroot.nohwrng") {
		os.Setenv("UROOT_NOHWRNG", "1")
		log.Printf("Deprecation warning: use UROOT_NOHWRNG=1 on kernel cmdline instead of uroot.nohwrng")
	}

	// Turn off job control when test mode is on.
	ctty := libinit.WithTTYControl(!*test)

	// Install modules before exec-ing into user mode below
	if err := libinit.InstallAllModules(); err != nil {
		log.Println(err)
	}

	// Allows passing args to uinit via kernel parameters, for example:
	//
	// uroot.uinitargs="-v --foobar"
	//
	// We also allow passing args to uinit via a flags file in
	// /etc/uinit.flags.
	args := cmdline.GetUinitArgs()
	if contents, err := os.ReadFile("/etc/uinit.flags"); err == nil {
		args = append(args, uflag.FileToArgv(string(contents))...)
	}
	uinitArgs := libinit.WithArguments(args...)

	return &initCmds{
		cmds: []*exec.Cmd{
			// inito is (optionally) created by the u-root command when the
			// u-root initramfs is merged with an existing initramfs that
			// has a /init. The name inito means "original /init" There may
			// be an inito if we are building on an existing initramfs. All
			// initos need their own pid space.
			libinit.Command("/inito", libinit.WithCloneFlags(syscall.CLONE_NEWPID), ctty),

			libinit.Command("/bbin/uinit", ctty, uinitArgs),
			libinit.Command("/bin/uinit", ctty, uinitArgs),
			libinit.Command("/buildbin/uinit", ctty, uinitArgs),

			libinit.Command("/bin/defaultsh", ctty),
			libinit.Command("/bin/sh", ctty),
		},
	}
}

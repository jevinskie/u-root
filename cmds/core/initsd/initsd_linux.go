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
	// systemd is "special". If we are supposed to run systemd, we're
	// going to exec, and if we're going to exec, we're done here.
	// systemd uber alles.
	initFlags := cmdline.GetInitFlagMap()

	// systemd gets upset when it discovers it isn't really process 1, so
	// we can't start it in its own namespace. I just love systemd.
	systemd, present := initFlags["systemd"]
	systemdEnabled, boolErr := strconv.ParseBool(systemd)
	log.Printf("systemd enabled: %t present: %t boolErr: %v", systemdEnabled, present, boolErr)
	if present && boolErr == nil && systemdEnabled {
		return true
	}
	return false
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

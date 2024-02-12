// Copyright 2012-2017 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// init is u-root's standard userspace init process.
//
// init is intended to be the first process run by the kernel when it boots up.
// init does some basic initialization (mount file systems, turn on loopback)
// and then tries to execute, in order, /inito, a uinit (either in /bin, /bbin,
// or /ubin), and then a shell (/bin/defaultsh and /bin/sh).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/u-root/u-root/pkg/curl"
	"github.com/u-root/u-root/pkg/libinit"
	"github.com/u-root/u-root/pkg/ulog"
)

// initCmds has all the bits needed to continue
// the init process after some initial setup.
type initCmds struct {
	cmds []*exec.Cmd
}

var (
	verbose    = flag.Bool("v", false, "Enable libinit debugging (includes showing commands that are run)")
	test       = flag.Bool("test", false, "Test mode: don't try to set control tty")
	rootfs_url = flag.String("rootfs_url", "", "rootfs URL")
	debug      = func(string, ...interface{}) {}
)

func kern_debug(s string, args ...interface{}) {
	ulog.KernelLog.Print(s)
	ulog.KernelLog.Print(args...)
	ulog.KernelLog.Print("\n!!!\n")
}

func print_mounts() {
	n := []string{"/proc/mounts"}
	for _, p := range n {
		b, err := os.ReadFile(p)
		if err == nil {
			log.Printf("mounts:\n%s", string(b))
		} else {
			log.Printf("Could not read %s to get namespace err: %v", p, err)
		}
	}
}

func main() {
	// ulog.KernelLog.SetLogLevel(ulog.KLogDebug)
	// ulog.KernelLog.SetConsoleLogLevel(ulog.KLogDebug)
	log.Println("printing init args:")
	log.Println(strings.Join(os.Args, " ! "))
	flag.Parse()

	log.Printf("Welcome to u-root (systemd jevinskie edition)!")
	fmt.Println(`                              _`)
	fmt.Println(`   _   _      _ __ ___   ___ | |_`)
	fmt.Println(`  | | | |____| '__/ _ \ / _ \| __|`)
	fmt.Println(`  | |_| |____| | | (_) | (_) | |_`)
	fmt.Println(`   \__,_|    |_|  \___/ \___/ \__|`)
	fmt.Println(`               SYSTEMD`)
	fmt.Println()

	log.SetPrefix("initsd: ")

	if *verbose {
		log.Println("verbose mode enabled")
		debug = log.Printf
	} else {
		log.Println("verbose mode disabled")
	}

	// Before entering an interactive shell, decrease the loglevel because
	// spamming non-critical logs onto the shell frustrates users. The logs
	// are still accessible through kernel logs buffers (on most kernels).
	// quiet()

	libinit.SetEnv()
	libinit.CreateRootfs()
	libinit.NetInit()

	// osInitGo wraps all the kernel-specific (i.e. non-portable) stuff.
	// It returns an initCmds struct derived from kernel-specific information
	// to be used in the rest of init.
	ic := osInitGo()

	systemdEnabled := isSystemdEnabled()
	log.Printf("systemdEnabled: %t", systemdEnabled)

	print_mounts()

	if *rootfs_url != "" {
		log.Printf("rootfs URL: %s", *rootfs_url)
		parsedURL, parseUrlErr := url.Parse(*rootfs_url)
		if parseUrlErr != nil {
			log.Printf("Error parsing rootfs URL: %v", parseUrlErr)
			goto rootfs_exec_failed
		}
		log.Printf("parsedURL: %v", parsedURL)
		schemes := curl.Schemes{
			"tftp": curl.DefaultTFTPClient,
			"http": curl.DefaultHTTPClient,

			// curl.DefaultSchemes doesn't support HTTPS by default.
			"https": curl.DefaultHTTPClient,
			"file":  &curl.LocalFileClient{},
		}

		_, err := schemes.FetchWithoutCache(context.Background(), parsedURL)
		if err != nil {
			log.Printf("failed to download %v: %w", *rootfs_url, err)
		}

	} else {
		log.Println("rootfs URL not specified")
	}

rootfs_exec_failed:

	cmdCount := libinit.RunCommands(debug, ic.cmds...)
	if cmdCount == 0 {
		log.Printf("No suitable executable found in %v", ic.cmds)
	}

	// We need to reap all children before exiting.
	log.Printf("Waiting for orphaned children")
	libinit.WaitOrphans()
	log.Printf("All commands exited")
	log.Printf("Syncing filesystems")
	if err := quiesce(); err != nil {
		log.Printf("%v", err)
	}
	log.Printf("Exiting...")
}

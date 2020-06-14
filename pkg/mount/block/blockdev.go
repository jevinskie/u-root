// Copyright 2017-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package block finds, mounts, and modifies block devices on Linux systems.
package block

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/rekby/gpt"
	"github.com/u-root/u-root/pkg/mount"
	"golang.org/x/sys/unix"
)

var (
	// LinuxMountsPath is the standard mountpoint list path
	LinuxMountsPath = "/proc/mounts"
)

// BlockDev maps a device name to a BlockStat structure for a given block device
type BlockDev struct {
	Name   string
	FSType string
	FsUUID string
}

// Device makes sure the block device exists and returns a handle to it.
//
// maybeDevpath can be path like /dev/sda1, /sys/class/block/sda1 or just sda1.
// We will just use the last component.
func Device(maybeDevpath string) (*BlockDev, error) {
	devname := filepath.Base(maybeDevpath)
	if _, err := os.Stat(filepath.Join("/sys/class/block", devname)); err != nil {
		return nil, err
	}

	devpath := filepath.Join("/dev/", devname)
	if uuid, err := getUUID(devpath); err == nil {
		return &BlockDev{Name: devname, FsUUID: uuid}, nil
	}
	return &BlockDev{Name: devname}, nil
}

// String implements fmt.Stringer.
func (b *BlockDev) String() string {
	if len(b.FSType) > 0 {
		return fmt.Sprintf("BlockDevice(name=%s, fs_type=%s, fs_uuid=%s)", b.Name, b.FSType, b.FsUUID)
	}
	return fmt.Sprintf("BlockDevice(name=%s, fs_uuid=%s)", b.Name, b.FsUUID)
}

// DevicePath is the path to the actual device.
func (b BlockDev) DevicePath() string {
	return filepath.Join("/dev/", b.Name)
}

// Mount implements mount.Mounter.
func (b *BlockDev) Mount(path string, flags uintptr) (*mount.MountPoint, error) {
	devpath := filepath.Join("/dev", b.Name)
	if len(b.FSType) > 0 {
		return mount.Mount(devpath, path, b.FSType, "", flags)
	}

	return mount.TryMount(devpath, path, flags)
}

// GPTTable tries to read a GPT table from the block device described by the
// passed BlockDev object, and returns a gpt.Table object, or an error if any
func (b *BlockDev) GPTTable() (*gpt.Table, error) {
	fd, err := os.Open(b.DevicePath())
	if err != nil {
		return nil, err
	}
	defer fd.Close()

	blkSize, err := b.BlockSize()
	if err != nil {
		return nil, fmt.Errorf("failed to find block size: %v", err)
	}

	if _, err := fd.Seek(int64(blkSize), io.SeekStart); err != nil {
		return nil, err
	}
	table, err := gpt.ReadTable(fd, uint64(blkSize))
	if err != nil {
		return nil, err
	}
	return &table, nil
}

// PhysicalBlockSize returns the physical block size.
func (b *BlockDev) PhysicalBlockSize() (int, error) {
	f, err := os.Open(b.DevicePath())
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return unix.IoctlGetInt(int(f.Fd()), unix.BLKPBSZGET)
}

// BlockSize returns the logical block size (BLKSSZGET).
func (b *BlockDev) BlockSize() (int, error) {
	f, err := os.Open(b.DevicePath())
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return unix.IoctlGetInt(int(f.Fd()), unix.BLKSSZGET)
}

// KernelBlockSize returns the soft block size used inside the kernel (BLKBSZGET).
func (b *BlockDev) KernelBlockSize() (int, error) {
	f, err := os.Open(b.DevicePath())
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return unix.IoctlGetInt(int(f.Fd()), unix.BLKBSZGET)
}

func ioctlGetUint64(fd int, req uint) (uint64, error) {
	var value uint64
	_, _, err := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(unsafe.Pointer(&value)))
	if err != 0 {
		return 0, err
	}
	return value, nil
}

// Size returns the size in bytes.
func (b *BlockDev) Size() (uint64, error) {
	f, err := os.Open(b.DevicePath())
	if err != nil {
		return 0, err
	}
	defer f.Close()

	sz, err := ioctlGetUint64(int(f.Fd()), unix.BLKGETSIZE64)
	if err != nil {
		return 0, &os.PathError{
			Op:   "get size",
			Path: b.DevicePath(),
			Err:  os.NewSyscallError("ioctl(BLKGETSIZE64)", err),
		}
	}
	return sz, nil
}

// ReadPartitionTable prompts the kernel to re-read the partition table on this block device.
func (b *BlockDev) ReadPartitionTable() error {
	f, err := os.OpenFile(b.DevicePath(), os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return unix.IoctlSetInt(int(f.Fd()), unix.BLKRRPART, 0)
}

// SystemPartitionGUID is the GUID of EFI system partitions
// EFI System partitions have GUID C12A7328-F81F-11D2-BA4B-00A0C93EC93B
var SystemPartitionGUID = gpt.Guid([...]byte{
	0x28, 0x73, 0x2a, 0xc1,
	0x1f, 0xf8,
	0xd2, 0x11,
	0xba, 0x4b,
	0x00, 0xa0, 0xc9, 0x3e, 0xc9, 0x3b,
})

// GetBlockDevices iterates over /sys/class/block entries and returns a list of
// BlockDev objects, or an error if any
func GetBlockDevices() (BlockDevices, error) {
	var blockdevs []*BlockDev
	var devnames []string

	root := "/sys/class/block"
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		devnames = append(devnames, rel)
		dev, err := Device(rel)
		if err != nil {
			return err
		}
		blockdevs = append(blockdevs, dev)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return blockdevs, nil
}

func getUUID(devpath string) (string, error) {
	file, err := os.Open(devpath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	fsuuid, err := tryVFAT(file)
	if err == nil {
		return fsuuid, nil
	}
	fsuuid, err = tryEXT4(file)
	if err == nil {
		return fsuuid, nil
	}
	fsuuid, err = tryXFS(file)
	if err == nil {
		return fsuuid, nil
	}
	return "", fmt.Errorf("unknown UUID (not vfat, ext4, nor xfs)")
}

// See https://www.nongnu.org/ext2-doc/ext2.html#DISK-ORGANISATION.
const (
	// Offset of superblock in partition.
	ext2SprblkOff = 1024

	// Offset of magic number in suberblock.
	ext2SprblkMagicOff  = 56
	ext2SprblkMagicSize = 2

	ext2SprblkMagic = 0xEF53

	// Offset of UUID in superblock.
	ext2SprblkUUIDOff  = 104
	ext2SprblkUUIDSize = 16
)

func tryEXT4(file io.ReaderAt) (string, error) {
	var off int64

	// Read magic number.
	b := make([]byte, ext2SprblkMagicSize)
	off = ext2SprblkOff + ext2SprblkMagicOff
	if _, err := file.ReadAt(b, off); err != nil {
		return "", err
	}
	magic := binary.LittleEndian.Uint16(b[:2])
	if magic != ext2SprblkMagic {
		return "", fmt.Errorf("ext4 magic not found")
	}

	// Filesystem UUID.
	b = make([]byte, ext2SprblkUUIDSize)
	off = ext2SprblkOff + ext2SprblkUUIDOff
	if _, err := file.ReadAt(b, off); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

// See https://de.wikipedia.org/wiki/File_Allocation_Table#Aufbau.
const (
	fat32Magic = "FAT32   "

	// Offset of magic number.
	fat32MagicOff  = 82
	fat32MagicSize = 8

	// Offset of filesystem ID / serial number. Treated as short filesystem UUID.
	fat32IDOff  = 67
	fat32IDSize = 4
)

func tryVFAT(file io.ReaderAt) (string, error) {
	// Read magic number.
	b := make([]byte, fat32MagicSize)
	if _, err := file.ReadAt(b, fat32MagicOff); err != nil {
		return "", err
	}
	magic := string(b)
	if magic != fat32Magic {
		return "", fmt.Errorf("fat32 magic not found")
	}

	// Filesystem UUID.
	b = make([]byte, fat32IDSize)
	if _, err := file.ReadAt(b, fat32IDOff); err != nil {
		return "", err
	}

	return fmt.Sprintf("%02x%02x-%02x%02x", b[3], b[2], b[1], b[0]), nil
}

const (
	xfsMagic     = "XFSB"
	xfsMagicSize = 4
	xfsUUIDOff   = 32
	xfsUUIDSize  = 16
)

func tryXFS(file io.ReaderAt) (string, error) {
	// Read magic number.
	b := make([]byte, xfsMagicSize)
	if _, err := file.ReadAt(b, 0); err != nil {
		return "", err
	}
	magic := string(b)
	if magic != xfsMagic {
		return "", fmt.Errorf("xfs magic not found")
	}

	// Filesystem UUID.
	b = make([]byte, xfsUUIDSize)
	if _, err := file.ReadAt(b, xfsUUIDOff); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

// BlockDevices is a list of block devices.
type BlockDevices []*BlockDev

// FilterZeroSize attempts to find block devices that have at least one block
// of content.
//
// This serves to eliminate block devices that have no backing storage, but
// appear in /sys/class/block anyway (like some loop, nbd, or ram devices).
func (b BlockDevices) FilterZeroSize() BlockDevices {
	var nb BlockDevices
	for _, device := range b {
		if n, err := device.Size(); err != nil || n == 0 {
			continue
		}
		nb = append(nb, device)
	}
	return nb
}

// FilterESP returns a list of BlockDev objects whose underlying block device
// is a valid EFI system partition.
func (b BlockDevices) FilterESP() BlockDevices {
	return b.FilterGUID(SystemPartitionGUID.String())
}

// FilterGUID returns a list of BlockDev objects whose underlying
// block device has the given GPT partition GUID.
func (b BlockDevices) FilterGUID(guid string) BlockDevices {
	partitions := make(BlockDevices, 0)
	for _, device := range b {
		table, err := device.GPTTable()
		if err != nil {
			log.Printf("Skipping; no GPT table on %s: %v", device.Name, err)
			continue
		}
		for _, part := range table.Partitions {
			if part.IsEmpty() {
				continue
			}
			if part.Type.String() == guid {
				partitions = append(partitions, device)
			}
		}
	}
	return partitions
}

// FilterFSUUID returns a list of BlockDev objects whose underlying block
// device has a filesystem with the given UUID.
func (b BlockDevices) FilterFSUUID(fsuuid string) BlockDevices {
	partitions := make(BlockDevices, 0)
	for _, device := range b {
		if device.FsUUID == fsuuid {
			partitions = append(partitions, device)
		}
	}
	return partitions
}

// FilterName returns a list of BlockDev objects whose underlying
// block device has a Name with the given Name
func (b BlockDevices) FilterName(name string) BlockDevices {
	partitions := make(BlockDevices, 0)
	for _, device := range b {
		if device.Name == name {
			partitions = append(partitions, device)
		}
	}
	return partitions
}

// GetMountpointByDevice gets the mountpoint by given
// device name. Returns on first match
func GetMountpointByDevice(devicePath string) (*string, error) {
	file, err := os.Open(LinuxMountsPath)
	if err != nil {
		return nil, err
	}

	defer file.Close()
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		deviceInfo := strings.Fields(scanner.Text())
		if deviceInfo[0] == devicePath {
			return &deviceInfo[1], nil
		}
	}

	return nil, errors.New("Mountpoint not found")
}
./u-root -no-strip -files "rootfs/sys/firmware/efi/efivars:sys/firmware/efi/efivars" -files "rootfs/bin/jev-uinit.sh:inito" all
&& xz --check=crc32 -9 --lzma2=dict=1MiB --stdout /tmp/initramfs.linux_amd64.cpio | dd conv=sync obs=512 of=/tmp/initramfs.linux_amd64.cpio.xz

qemu-system-x86_64 -enable-kvm -machine q35 -cpu host -kernel $HOME/code/linux/easylkb/kernel/linux-6.7.2/arch/x86/boot/bzImage -initrd /tmp/initramfs.linux_amd64.cpio.xz -nographic -m 2G -smp 2 -net user,host=10.0.2.10,hostfwd=tcp:127.0.0.1:10021-:22 -net nic,model=e1000 -append "console=ttyS0 earlyprintk=serial ip=dhcp -v"

./u-root -no-strip -initcmd /bbin/initsd all

qemu-system-x86_64 -enable-kvm -machine q35 -cpu host -kernel $HOME/code/linux/easylkb/kernel/linux-6.7.2/arch/x86/boot/bzImage -initrd /tmp/initramfs.linux_amd64.cpio -nographic -m 2G -smp 2 -net user,host=10.0.2.10,dns=192.168.1.10,hostfwd=tcp:127.0.0.1:10021-:22 -net nic,model=e1000 -append "console=ttyS0 earlyprintk=serial ip=dhcp debug -- -v -rootfs_url=http://192.168.1.12/rootfs.squashfs"

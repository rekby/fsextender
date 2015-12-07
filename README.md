[![Build Status](https://travis-ci.org/rekby/fsextender.svg)](https://travis-ci.org/rekby/fsextender)
[![Coverage Status](https://coveralls.io/repos/rekby/fsextender/badge.svg?branch=master&service=github)](https://coveralls.io/github/rekby/fsextender?branch=master)

Extend filesystem to max size.

If filesystem lie on LVM-volume - extend lvm volume and lvm volume group to max size too (extend partitions, create new
partitions and etc).

Usage example:
fsextender /home [--do]

--do - do modify partitions (without print plan).
Without --do - print plan.

Detect result:
OK - if extended compele. Return code 0.
NEED REBOOT AND START ME ONCE AGAIN. - if need reboot and run command with same parameters. Return code 1.

external dependencies:
/proc/mounts - detect mount points
/sys/

blkid - detect file system type
stat - detect major,minor number of device
blockdev - get sector size of disk - need for manipulate with partition tables.
partprobe - reread partition table after changes. TODO: replace with blockdev --rereadadpt

#!/bin/bash

./_clean.sh 2>/dev/null || exit 1

parted -ms /dev/sdb unit s mkpart primary 9764864 11718655 # 5G-6G
parted -ms /dev/sdb set 1 lvm on
pvcreate /dev/sdb1
vgcreate tlvm /dev/sdb1
lvcreate -L 500M -n test tlvm
mkfs.ext4 /dev/tlvm/test
mount /dev/tlvm/test /mnt/test
echo OK > /mnt/test/ok


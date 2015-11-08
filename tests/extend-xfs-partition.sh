#!/bin/bash

./_clean.sh 2>/dev/null || exit 1

parted -ms /dev/sdb unit s mkpart primary 34 2048034
mkfs.xfs /dev/sdb1
mount /dev/sdb1 /mnt/test
echo OK > /mnt/test/ok


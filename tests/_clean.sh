#!/bin/bash

if [ "$FSEXTENDER_TEST" != "1" ]; then
    echo "DATA LOST PROTECTION"
    echo -e 'DO\nexport FSEXTENDER_TEST=1\nthen run again'
    exit 1
fi

if [ "$PARTTYPE" == "" ]; then
    echo -e "export PARTTYPE=msdos\nor\nexport PARTTYPE=gpt"
    exit 1
fi

# umount test filesystems
if [ -e /mnt/test ]; then
    umount -f /mnt/test
fi

rm -rf /mnt/test
mkdir -p /mnt/test

# remove lvm
vgremove -f tlvm

# remove pv, clean partition from metadata
for i in `find /dev/ -maxdepth 1 -mindepth 1 -regex '/dev/sdb.+'`; do
    pvremove -ff $i
    dd if=/dev/zero of=$i bs=1M count=1
    parted /dev/sdb rm ${i#/dev/sdb}
done

# remove partition table
dd if=/dev/zero of=/dev/sdb bs=1M count=1

parted -sm /dev/sdb mklabel $PARTTYPE

exit 0

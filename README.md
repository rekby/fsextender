[comment]: <> ([![Coverage Status]&#40;https://coveralls.io/repos/rekby/fsextender/badge.svg?branch=master&service=github&#41;]&#40;https://coveralls.io/github/rekby/fsextender?branch=master&#41;)

[comment]: <> ([![Build Status]&#40;https://travis-ci.org/rekby/fsextender.svg&#41;]&#40;https://travis-ci.org/rekby/fsextender&#41;)

[comment]: <> (Test status doesn't actual because based on old unsupported ubuntu version. )

Extend filesystem to max size with underliing layers.
It can extend: ext3, ext4, xfs, LVM Logical volume, LVM Physical volume, LVM Volume Group (with new or free pv)
, partitions in MSDOS and GPT partition tables.
It can create new partitions and LVM Physical volumes on disk with MSDOS and GPT partition tables.

Расширяет файловую систему до максимального размера, вместе с нижележащими слоями.
Может расширять: ext3, ext4, xfs, логические и физические тома LVM, LVM Volume Group (за счет создания новых
физических томов и использования уже созданных, но свободных), разделы на дисках с таблицами разделов MSDOS
и GPT.
Может создавать: новые разделы и физические тома LVM на дисках с таблицами разделов MSDOS и GPT.

Usage example:
Пример использования:
fsextender [--filter=LVM_ALREADY_PLACED] /home [--do]

Instruction see in usage.txt
Инструкцию смотрите в usage.txt

external dependencies:
Внешние зависимости:

/proc/mounts - detect mount points
/sys/

blkid - detect file system type
stat - detect major,minor number of device
blockdev - get sector size of disk - need for manipulate with partition tables.
partprobe - reread partition table after changes. TODO: replace with blockdev --rereadadpt

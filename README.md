[![Build Status](https://travis-ci.org/rekby/fsextender.svg)](https://travis-ci.org/rekby/fsextender)
[![Coverage Status](https://coveralls.io/repos/rekby/fsextender/badge.svg?branch=master&service=github)](https://coveralls.io/github/rekby/fsextender?branch=master)

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

--do - do modify partitions (without print plan).
       Without --do - print plan.
       
       Применить изменения
       Без --do - печатается план изменений. но никаких операций не выполняется

--filter, -f - filter block devices for extend LVM volume group.
               if it equal LVM_ALREADY_PLACED (default) - LVM vg extend only by space on disk
               in which PV of the lvm VG already placed. It can create new partitions and extend existed.
               If you want no limit to extend LVM use empty filter: --filter=
               If you want own limit: write regexp with your rules. You can use many rules, separated
               by comma. For example: --filter=/dev/sda,/dev/sdb
               If rule starts with / - it mean absolute path and rule prepend with ^ (for example /dev/sda.*
               replaced to ^/dev/sda.*)
               If rule doesn't contain any of mask characters: ^*+?[]
               it is appended with [^/]*$ which mean - ends with any characters, but folder separator.
               For example: /dev/sda will be replaced to ^/dev/sda[^/]*$
               
               If volume group already placed in disk, that ignored by filter - the PVs in ignored drive
               won't be extend, but free space in the PV will be user for extend LVM Volume.
               
               Фильтровать блочные устройства, за счет которых может расширяться LVM VolumeGroup.
               Если равно LVM_ALREADY_PLACED (по умолчанию) - LVM может расширяться тольно за счет
               тех дисков, где он уже находится (могут как создаваться новые разделы, так и расширяться
               существующие).
               Если вы хотите отменить все ограничения для расширения LVM используйте пустой фильтр: --filter=
               Если вы хотите задать свои правила ограничений: напишите регулярные выражения с вашими правилами.
               Несколько выражений можно перечислять через запятую: --filter=/dev/sda,/dev/sdb
               Если правило начинается с / - это понимается как адсолютный путь и перед правилом добавится ^, т.е.
               правило /dev/sda.* заменится на ^/dev/sda.*
               Если исходноеп правило не содержит спец. символов регулярных выражений: ^*+?[]
               то правило дополнится строкой [^/]$, что означает - любые символы, кроме разделителя папок.
               Например /dev/sda будет заменено на ^/dev/sda[^/]*$

Detect result:
Проверка результата расширения.

Stdout: OK
Печать на стандартный вывод: ОК
     Extended compele. Return code 0.
     Расширение завершено успешно. Код возврата 0.

Stdout: NEED REBOOT AND START ME ONCE AGAIN.
Печать на стандартный вывод: NEED REBOOT AND START ME ONCE AGAIN.
     Need reboot and run command with same parameters. Return code 128.
     Нужно перезагрузить ОС и запустить расширитель с теми же параметрами для завершения работы. Код возврата 128.

0 < Code < 128 mean error exit.
0 < Код возврата < 128 - означает ошибку выполнения.

external dependencies:
Внешние зависимости:

/proc/mounts - detect mount points
/sys/

blkid - detect file system type
stat - detect major,minor number of device
blockdev - get sector size of disk - need for manipulate with partition tables.
partprobe - reread partition table after changes. TODO: replace with blockdev --rereadadpt

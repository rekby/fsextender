Program for extend partitions with lvm/filesystem on that.
Программа для расширения разделов диска и lvm-томов/файловых систем на этих разделах.

It support MBR primary (non extended) partitions only now.
Сейчас поддерживаются только первичные (не расширенные) MBR-разделы.

Usage: fsextender <path_to_part>|<path_to_fs>
Extend mbr partition and LVM physical volume/filesystem to max size.
Расширяет раздел на MBR-диске и физический том LVM или файловую систему этого раздела до максимального размера.

It support only primary MBR partitions now.
На данный момент утилита поддерживает расширение только основных (не расширенных) разделов на MBR-дисках.

path_to_part - path to device partition, which need to be extended: /dev/sdb1, /dev/hda2 ...
  in the case - extend block device to max size and upper level on device.
  It can be filesystem or physycal volume of LVM.
path_to_part - указывается путь к разделу диска, который нужно расширить, например: /dev/sdb1, /dev/hda2
  в этом случае расширяется указанный раздел и то что на нем лежит (файловая система или физический том LVM).

path_to_fs - path to filesystem, need to be extended: /home, /var/lib, ...
  in the case - extend all underly block devices and fs.
  It can be path to LVM-volume with filesystem.
path_to_fs - путь к файловой системе, которую нужно расширить: /home, /var/lib, ...
  в этом случае расширяются все нижележащие слои LVM если они есть, разделы для них (или файловой системы) и затем
  сама файловая система.
  Может указываться путь к LVM-тому с файловой системой.

Difference:
  When filesystem is on top of common disk partition both variants identical.

  LVM differenct:
    path_to_dev - partition and physican volume will be expanded, but no filesystem be extended. Becouse it
     can have number filesystem and fsextender can't know about what filesystem you want extend.
    path_to_fs - will extend under levels partitions, then extend the filesystem.
     It see /etc/mtab for block device for the filesystem, then detect if it is LVM - try to extend ALL physical
     volumes for storage group of LVM, then extend logical volume and the filesystem.

Различия:
  Когда файловая система лежит непосредственно на дисковом разделе оба варианта работают идентично.

  Различия при использовании LVM:
    path_to_dev - расширяется указанный раздел и физический том LVM, который на нем находится. При этом файловые
      системы LVM не трогаются - их может быть много и fsextender не может угадать какую именно вы хотите расширить.
    path_to_fs - проверяется /etc/mtab для понимания блочного устройства на котором расположена файловая система и если
      это LVM - делается попытка расширить все тома Volume Group в которую входит логический том с файловой системой,
      затем расширяется логический том и непосредствено файловая система

OUTPUT:
  OK: COMPLETE - resize compele OK.
  OK: NOTHING - partition/filesystem is max size already.
  NEEDREBOOT - need reboot OS for compete resizind.
  ERROR: ... - have error.

Вывод:
  OK: COMPLETE - изменение размера раздела/файловой системы завершенно успешно.
  OK: NOTHING - раздел/файловая система уже максимального размера, ничего делать не нужно.
  NEEDREBOOT - Для завершения операции требуется перезагрузка системы.
  ERROR: ... - Произошла ошибка

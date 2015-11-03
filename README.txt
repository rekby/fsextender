Extend filesystem to max size.

If filesystem lie on LVM-volume - extend lvm volume and lvm volume group to max size too (extend partitions, create new
partitions and etc).

Usage example:
fsextender /home [--do]

--do - do modify partitions. Without --do - print extend plan only.

it write to stdout:
OK - if extended compele.
NEED REBOOT AND START ME ONCE AGAIN. - if need reboot and run command with same parameters

external dependencies:
/proc/mounts - detect mount points
/sys/

blkid - detect file system type
stat - detect major,minor number of device
parted - manipulate with partition tables
package fsextender

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"bufio"
	mmbr "github.com/rekby/mbr"
	"io/ioutil"
	"path/filepath"
)

var didNothing bool = true
var needReboot bool = false

func Main() {
	defer func() {
		err := recover()
		if err != nil {
			fmt.Printf("ERROR: %s\n", err.(error).Error())
			return
		}

		if needReboot {
			writeRebootScript()
			fmt.Println("NEEDREBOOT")
			return
		}

		if didNothing {
			fmt.Println("OK: NOTHING")
		} else {
			fmt.Println("OK: COMPLETE")
		}
	}()
	if len(os.Args) != 2 {
		printUsage()
	}

	path := os.Args[1]
	stat, err := os.Stat(path)
	if err != nil {
		doErrMess("Can't stat file: " + path)
		return
	}

	switch {
	case stat.Mode()&os.ModeDir == os.ModeDir:
		devName := getBlockDevice(path)
		extendBlockDevice(devName)
	case stat.Mode()&os.ModeDevice == os.ModeDevice:
		extendBlockDevice(path)
	default:
		doErrMess("Parameter must be path to mount point of filesystem or partition or lvm-volume.")
		return
	}
}

/*
Run external command, return string result and error (exit status code)
*/
func cmd(path string, args ...string) (string, error) {
	command := exec.Command(path, args...)
	command.Stderr = os.Stderr
	var buf bytes.Buffer
	command.Stdout = &buf
	err := command.Run()
	res := string(buf.Bytes())
	return res, err
}

/*
Detect if path filesystem placed in lvm volume.
If is - return group and volume name for the filesystem.
If not - return "", ""
call doErr if errors.
*/
func detectLVMVolume(path string) (group, volume string) {
	// get full path to target file
	readlink := func(path string) string {
		for {
			stat, err := os.Lstat(path)
			if err != nil {
				doErr(err)
				return ""
			}
			if stat.Mode()&os.ModeSymlink == os.ModeSymlink {
				linkTarget, err := os.Readlink(path)
				path = filepath.Join(filepath.Dir(path), linkTarget)
				if err != nil {
					doErr(err)
					return ""
				}
			} else {
				return path
			}
		}
	}

	// get path for filesystem's device
	diskName := readlink(path)

	// read all lvm volumes
	// check if one of lvm volumes link to same device as path
	res, err := cmd("lvs", "--noheadings", "--separator", "|", "-o", "vg_name,lv_name")
	if err != nil {
		doErr(err)
		return
	}
	for _, line := range strings.Split(res, "\n") {
		line = strings.TrimSpace(line)
		lineParts := strings.Split(line, "|")
		if len(lineParts) < 2 {
			doErrMess("Error while parse lvs list")
			return
		}
		vgNameTmp, lvNameTmp := lineParts[0], lineParts[1]
		if readlink(filepath.Join("/dev", vgNameTmp, lvNameTmp)) == diskName {
			return vgNameTmp, lvNameTmp
		}
	}
	return
}

func doErrMess(mess string) {
	panic(errors.New(mess))
}

func doErr(err error) {
	panic(err)
}

func extendBlockDevice(devName string){
	lvgroup, lvvolume := detectLVMVolume(devName)

	// if it is NOT LVM
	if lvgroup == "" {
		extendPartition(devName)
		return
	}

	// if it IS LVM
	// detect and extend all physical volume
	res, err := cmd("pvs", "--noheading", "--separator", "|", "-o", "vg_name,pv_name")
	if err != nil {
		doErr(err)
		return
	}
	for _, line := range strings.Split(res, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lineParts := strings.Split(line, "|")
		if len(lineParts) < 2 {
			doErrMess("Error while parse pvs.")
			return
		}
		vgName, pvName := lineParts[0], lineParts[1]
		if vgName != lvgroup {
			// extend only partitions under the filesystem
			continue
		}

		extendPartition(pvName)
	}

	// Extend filesystem if volume group have free extents
	res, err = cmd("vgs", "--noheading", "-o", "vg_free_count", lvgroup)
	if err != nil {
		doErr(err)
		return
	}
	res = strings.TrimSpace(res)
	if res != "0" {
		didNothing = false
		_, err = cmd("lvresize", "-l", "+100%FREE", lvgroup+"/"+lvvolume)
		if err != nil {
			doErr(err)
		}
	}

	extendFileSystem(devName)
}

/*
Extend ext 3,4 partitions
Return:
  true - if filesystem was extended
  false - if filesystem has max size already
call doErr if errors
*/
func extendExt(partPath string) bool {
	// Force check filesystem before extend
	cmd("e2fsck", "-f", partPath)

	res, err := cmd("resize2fs", partPath)
	if err != nil {
		doErrMess("Can't resize2f2 " + partPath + ": " + err.Error())
	}
	return !strings.Contains(res, "Nothing to do")
}

func getBlockDevice(path string)string {
	// check if it is mountpoint
	mtab, err := os.Open("/etc/mtab")
	defer mtab.Close()
	if err != nil {
		doErr(err)
	}

	var devName string = ""
	reader := bufio.NewScanner(mtab)
	for reader.Scan() {
		lineParts := strings.Split(reader.Text(), " ")
		if len(lineParts) < 2 {
			continue
		}
		if lineParts[1] == path {
			devName = lineParts[0]
			break
		}
	}
	if reader.Err() != nil {
		doErr(reader.Err())
	}
	if devName == "" {
		doErrMess("Parameter must be path to mount point of filesystem or partition (2).")
		return ""
	}

	return devName
}

func extendFileSystem(devName string) {
	// detect upper level storage:
	res, err := cmd("blkid", devName)
	if err != nil && err.Error() != "exit status 2" {
		doErrMess("Can't detect what stored on " + devName + ": " + err.Error())
		return
	}

	res = strings.TrimSpace(res)
	switch {
	case res == "": // Empty partition
		return
	case strings.Contains(res, `TYPE="LVM2_member"`): // LVM Physical volume
		if extendLVMPhysicalVolume(devName) {
			didNothing = false
		}
	case strings.Contains(res, `TYPE="xfs"`): // Xfs file system
		if extendXFS(devName) {
			didNothing = false
		}
	case strings.Contains(res, `TYPE="ext3"`) || strings.Contains(res, `TYPE="ext4"`): // Ext-family with online resize support
		if extendExt(devName) {
			didNothing = false
		}
	default:
		doErrMess("Can't detect or unsupported file system of " + devName + " (" + res + ")")
		return
	}
}

/*
Extend physical volume.
Return:
  true if physical volume was extended.
  false if physical volume has max size already
call doErr on errors.
*/
func extendLVMPhysicalVolume(path string) bool {
	oldSize, err := cmd("pvs", "--nosuffix", "--noheadings", "--units", "b", "-o", "pv_size", path)
	if err != nil {
		doErr(err)
	}

	_, err = cmd("pvresize", path)
	if err != nil {
		doErr(err)
	}

	newSize, err := cmd("pvs", "--nosuffix", "--noheadings", "--units", "b", "-o", "pv_size", path)
	if err != nil {
		doErr(err)
	}

	return newSize != oldSize
}

func extendPartition(path string) {
	var err error

	if extendPartitionMBR(path) {
		didNothing = false
		diskPath := path[:len(path)-1]
		_, err = cmd("blockdev", "--rereadpt", diskPath)
		if err != nil {
			fmt.Println(err.Error())
			needReboot = true
			return
		}
	}

	extendFileSystem(path)
}

/*
Extend partition on physical disk.
Return true if partition was extended, false - when partition is max size already.
call doErr if some errors
*/
func extendPartitionMBR(path string) bool {
	// Get parent block device
	diskPath := path[:len(path)-1]

	// Detect block device size
	diskSize := getSizeBlockDev(diskPath)

	// Read MBR
	disk, err := os.OpenFile(diskPath, os.O_RDWR|os.O_SYNC, 0600)
	defer disk.Close()
	if err != nil {
		doErr(err)
	}
	mbr, err := mmbr.Read(disk)
	if err != nil {
		doErr(err)
	}

	// Detect if partition can grow
	partitionNum, err := strconv.Atoi(path[len(path)-1:])
	if err != nil {
		doErr(err)
	}
	partition := mbr.GetPartition(partitionNum)
	if partition.IsEmpty() {
		doErrMess("I can't to grow empty partition.")
	}
	oldSize := partition.GetLBALen()

	newSize := diskSize - partition.GetLBAStart()
	for i := 1; i <= 4; i++ {
		pOther := mbr.GetPartition(i)
		if pOther.GetLBAStart() > partition.GetLBAStart() {
			currentTestSize := pOther.GetLBAStart() - partition.GetLBAStart()
			if currentTestSize < newSize {
				newSize = currentTestSize
			}
		}
	}
	if newSize < oldSize {
		doErrMess("Detect newsize of partition have to smaller, then oldsize: " + path)
	}
	if newSize == oldSize {
		return false
	}

	// if newSize > oldSize: ...
	partition.SetLBALen(newSize)
	if mbr.Check() != nil {
		doErr(mbr.Check())
		return false
	}

	_, err = disk.Seek(0, 0)
	if err != nil {
		doErr(err)
	}
	err = mbr.Write(disk)
	if err != nil {
		doErr(err)
	}
	return true
}

/*
Extend xfs partition.
Return:
  true - if filesystem was extended
  false - if filesystem has max size already
call doErr if errors.
*/
func extendXFS(partPath string) bool {
	var mountPath string = ""

	// Detect if part mounted
	mtab, err := os.Open("/etc/mtab")
	defer mtab.Close()
	if err != nil {
		doErrMess("Can't read /etc/mtab: " + err.Error())
		return false
	}
	scanner := bufio.NewScanner(mtab)
	for scanner.Scan() {
		line := scanner.Text()
		lineParts := strings.Split(line, " ")
		if len(lineParts) < 2 {
			continue
		}
		if lineParts[0] == partPath {
			mountPath = lineParts[1]
			break
		}
	}
	if scanner.Err() != nil {
		doErrMess("Can't read mtab(2): " + err.Error())
		return false
	}

	// If patition not mounted yet - need tmpMount
	if mountPath == "" {
		mountPath, err = ioutil.TempDir("", "fsextender_")
		if err != nil {
			doErrMess("Can't create tmp dir for mount xfs: " + err.Error())
			return false
		}
		defer os.Remove(mountPath)
		_, err = cmd("mount", "-t", "xfs", partPath, mountPath)
		if err != nil {
			doErrMess("Can't mount xfs to tmp dir: " + err.Error())
			return false
		}
		defer cmd("umount", "-f", mountPath)
	}

	res, err := cmd("xfs_growfs", mountPath)
	if err != nil {
		doErrMess("can't exec xfs_growfs " + mountPath + ": " + err.Error())
		return false
	}
	return strings.Contains(res, "data blocks changed from") // data blocks changed from 262144 to 524288
}

func getSizeBlockDev(path string) uint32 {
	res, err := cmd("blockdev", "--getsz", path)
	if err != nil {
		doErr(err)
		return 0
	}

	size64, err := strconv.ParseUint(strings.TrimSpace(res), 10, 32)
	if err != nil {
		doErr(err)
	}
	return uint32(size64)
}

func printUsage() {
	fmt.Println(`Usage: fsextender <path_to_part>|<path_to_fs>
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
`)
}

func writeRebootScript() {
	res, err := cmd("runlevel")
	if err != nil {
		doErrMess("Can't detect runlevel")
		return
	}
	levels := strings.Split(res, " ")
	if len(levels) != 2 {
		doErrMess("Can't detect runlevel (2)")
		return
	}

	currentLevel := strings.TrimSpace(levels[1])
	if currentLevel == "N" {
		doErrMess("Can't start on N level")
		return
	}
	dir := "/etc/rc" + currentLevel + ".d"

	scriptName := filepath.Join(dir, "S99_zzz_1gb_fsextender")
	scriptFile, err := os.OpenFile(scriptName, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0700)
	defer scriptFile.Close()
	if err != nil {
		doErr(err)
		return
	}

	selfName, err := filepath.Abs(os.Args[0])
	if err != nil {
		doErr(err)
		return
	}
	selfCallString := selfName + " " + os.Args[1]
	_, err = scriptFile.WriteString("#!/bin/bash\n" + selfCallString + "\n" + "rm -f " + scriptName + "\n")
	if err != nil {
		doErr(err)
		return
	}
}

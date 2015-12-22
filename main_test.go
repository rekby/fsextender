package fsextender

import (
	"fmt"
	"github.com/rekby/pflag"
	"github.com/rekby/pretty"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const GB = 1024 * 1024 * 1024
const TMP_DIR = "/tmp"
const TMP_MOUNT_DIR = "/tmp/fsextender-test-mount-dir"
const LVM_VG_NAME = "test-fsextender-lvm-vg"
const LVM_LV_NAME = "test-fsextender-lvm-lv"

const GPT_SIZE = 16896 // Size of GPT header + gpt entries
const TMP_DISK_SIZE = 100*GB
const MSDOS_START_BYTE = 32256
const MSDOS_LAST_BYTE = 107374182399
const GPT_START_BYTE = 512 + GPT_SIZE
const GPT_LAST_BYTE = TMP_DISK_SIZE - GPT_SIZE - 1

var PART_TABLES = []string{"msdos", "gpt"}

type testPartition struct {
	Number uint64
	Start  uint64
	Last   uint64
}

func resetProgramState() {
	pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.ExitOnError)
	majorMinorDeviceTypeCache = make(map[[2]int]storageItem)
	diskNewPartitionNumLastGeneratedNum = make(map[[2]int]uint32)
}

// Call main program
func call(args ...string) {
	if os.Getuid() == 0 {
		// for test coverage run test as root
		oldArgs := os.Args
		os.Args = append([]string{os.Args[0]}, args...)
		// Clean old environment
		resetProgramState()

		Main()
		os.Args = oldArgs
	} else {
		sudo("./fsextender", args...)
	}
}

// Create loop-back test device with partition table
func createTmpDevice(partTable string) (path string, err error) {
	var f *os.File
	f, err = ioutil.TempFile(TMP_DIR, "fsextender-loop-")
	if err != nil {
		return
	}
	fname := f.Name()

	// Large sparse size = 100GB
	_, err = f.WriteAt([]byte{0}, TMP_DISK_SIZE)
	f.Close()
	if err != nil {
		os.Remove(fname)
		return
	}

	res, errString, err := sudo("losetup", "-f", "--show", fname)
	path = strings.TrimSpace(res)
	if path == "" {
		err = fmt.Errorf("Can't create loop device: %v\n%v\n%v\n", fname, err, errString)
		os.Remove(fname)
		return
	}

	time.Sleep(time.Second)

	_, _, err = sudo("parted", "-s", path, "mklabel", partTable)
	if err != nil {
		err = fmt.Errorf("Can't create part table: %v (%v)\n", path, err)
		deleteTmpDevice(path)
		path = ""
		return
	}
	return path, nil
}

func deleteTmpDevice(path string) {
	filePath, _, _ := sudo("losetup", path)
	start := strings.Index(filePath, "(")
	finish := strings.Index(filePath, ")")
	filePath = filePath[start+1 : finish]

	// remove partitions
	for _, part := range readPartitions(path) {
		sudo("parted", "-s", path, "rm", strconv.Itoa(int(part.Number)))
	}

	sudo("sudo", "losetup", "-d", path)
	os.Remove(filePath)

	// Wait for umount device
	for {
		_, errString, _ := sudo("losetup", path)
		if errString != "" {
			break
		}
		time.Sleep(time.Second / 10) // time to kernel remove device
	}
}

// Return volume of filesystem in 1-Gb blocks
func df(path string) uint64 {
	res, _, _ := cmd("df", "-BG", path)
	resLines := strings.Split(res, "\n")
	blocksStart := strings.Index(resLines[0], "1G-blocks")
	blocksEnd := blocksStart + len("1G-blocks")
	blocksString := strings.TrimSpace(resLines[1][blocksStart:blocksEnd])
	blocksString = strings.TrimSuffix(blocksString, "G")
	blocks, _ := parseUint(blocksString)
	return blocks
}

func readPartitions(path string) (res []testPartition) {
	partedRes, _, _ := cmd("sudo", "parted", "-s", path, "unit", "b", "print")
	lines := strings.Split(partedRes, "\n")
	var numStart, numFinish, startStart, startFinish, endStart, endFinish int
	var startParse = false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "Number") {
			numStart = 0
			numFinish = strings.Index(line, "Start")
			startStart = strings.Index(line, "Start")
			startFinish = strings.Index(line, "End")
			endStart = strings.Index(line, "End")
			endFinish = strings.Index(line, "Size")
			startParse = true
			continue
		}
		if !startParse {
			continue
		}
		var partition testPartition
		partition.Number, _ = parseUint(strings.TrimSpace(line[numStart:numFinish]))
		if partition.Number == 0 {
			continue
		}
		partition.Start, _ = parseUint(strings.TrimSuffix(strings.TrimSpace(line[startStart:startFinish]), "B"))
		partition.Last, _ = parseUint(strings.TrimSuffix(strings.TrimSpace(line[endStart:endFinish]), "B"))

		res = append(res, partition)
	}
	return
}

func s(n uint64) string {
	return strconv.FormatUint(n, 10)
}

func sudo(command string, args ...string) (res string, errString string, err error) {
	if os.Getuid() == 0 {
		return cmd(command, args...)
	} else {
		args = append([]string{command}, args...)
		return cmd("sudo", args...)
	}
}

func TestExt4PartitionMSDOS(t *testing.T) {
	disk, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(MSDOS_START_BYTE), s(MSDOS_START_BYTE+GB)) // 1Gb
	part := disk + "p1"
	sudo("mkfs.ext4", part)
	err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
	if err == nil {
		defer os.Remove(TMP_MOUNT_DIR)
	} else {
		t.Fatal(err)
	}

	sudo("mount", part, TMP_MOUNT_DIR)
	defer sudo("umount", part)
	sudo("chmod", "a+rwx", TMP_MOUNT_DIR)
	err = ioutil.WriteFile(filepath.Join(TMP_MOUNT_DIR, "test"), []byte("OK"), 0666)
	if err != nil {
		t.Error("Can't write test file", err)
	}
	call(TMP_MOUNT_DIR, "--do")
	res, _, _ := cmd("df", "-BG", part)
	resLines := strings.Split(res, "\n")
	blocksStart := strings.Index(resLines[0], "1G-blocks")
	blocksEnd := blocksStart + len("1G-blocks")
	blocksString := strings.TrimSpace(resLines[1][blocksStart:blocksEnd])
	blocksString = strings.TrimSuffix(blocksString, "G")
	blocks, _ := parseUint(blocksString)
	if blocks != 99 {
		t.Error(resLines[1])
	}

	needPartitions := []testPartition{
		{1, MSDOS_START_BYTE, MSDOS_LAST_BYTE},
	}

	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
	}

	testBytes, err := ioutil.ReadFile(filepath.Join(TMP_MOUNT_DIR, "test"))
	if err != nil {
		t.Error("Can't read test file", err)
	}
	if string(testBytes) != "OK" {
		t.Error("Bad file content:", string(testBytes))
	}
}

func TestExt4PartitionGPT(t *testing.T) {
	disk, err := createTmpDevice("gpt")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(GPT_START_BYTE), s(GPT_START_BYTE+GB)) // 1Gb
	part := disk + "p1"
	sudo("mkfs.ext4", part)
	err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
	if err == nil {
		defer os.Remove(TMP_MOUNT_DIR)
	} else {
		t.Fatal(err)
	}

	sudo("mount", part, TMP_MOUNT_DIR)
	defer sudo("umount", part)
	sudo("chmod", "a+rwx", TMP_MOUNT_DIR)
	err = ioutil.WriteFile(filepath.Join(TMP_MOUNT_DIR, "test"), []byte("OK"), 0666)
	if err != nil {
		t.Error("Can't write test file", err)
	}
	call(TMP_MOUNT_DIR, "--do")
	res, _, _ := cmd("df", "-BG", part)
	resLines := strings.Split(res, "\n")
	blocksStart := strings.Index(resLines[0], "1G-blocks")
	blocksEnd := blocksStart + len("1G-blocks")
	blocksString := strings.TrimSpace(resLines[1][blocksStart:blocksEnd])
	blocksString = strings.TrimSuffix(blocksString, "G")
	blocks, _ := parseUint(blocksString)
	if blocks != 99 {
		t.Error(resLines[1])
	}

	needPartitions := []testPartition{
		{1, GPT_START_BYTE, GPT_LAST_BYTE},
	}
	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
	}

	testBytes, err := ioutil.ReadFile(filepath.Join(TMP_MOUNT_DIR, "test"))
	if err != nil {
		t.Error("Can't read test file", err)
	}
	if string(testBytes) != "OK" {
		t.Error("Bad file content:", string(testBytes))
	}

}

func TestXfsPartitionMSDOS(t *testing.T) {
	disk, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(MSDOS_START_BYTE), s(MSDOS_START_BYTE+GB)) // 1Gb
	part := disk + "p1"
	sudo("mkfs.xfs", part)
	err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
	if err == nil {
		defer os.Remove(TMP_MOUNT_DIR)
	} else {
		t.Fatal(err)
	}

	sudo("mount", part, TMP_MOUNT_DIR)
	defer sudo("umount", part)
	sudo("chmod", "a+rwx", TMP_MOUNT_DIR)
	err = ioutil.WriteFile(filepath.Join(TMP_MOUNT_DIR, "test"), []byte("OK"), 0666)
	if err != nil {
		t.Error("Can't write test file", err)
	}
	call(TMP_MOUNT_DIR, "--do")
	res, _, _ := cmd("df", "-BG", part)
	resLines := strings.Split(res, "\n")
	blocksStart := strings.Index(resLines[0], "1G-blocks")
	blocksEnd := blocksStart + len("1G-blocks")
	blocksString := strings.TrimSpace(resLines[1][blocksStart:blocksEnd])
	blocksString = strings.TrimSuffix(blocksString, "G")
	blocks, _ := parseUint(blocksString)
	if blocks != 100 {
		t.Error(resLines[1])
	}

	needPartitions := []testPartition{
		{1, MSDOS_START_BYTE, MSDOS_LAST_BYTE},
	}
	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
	}
	testBytes, err := ioutil.ReadFile(filepath.Join(TMP_MOUNT_DIR, "test"))
	if err != nil {
		t.Error("Can't read test file", err)
	}
	if string(testBytes) != "OK" {
		t.Error("Bad file content:", string(testBytes))
	}
}

func TestXfsPartitionGPT(t *testing.T) {
	disk, err := createTmpDevice("gpt")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(GPT_START_BYTE), s(GPT_START_BYTE+GB)) // 1Gb
	part := disk + "p1"
	sudo("mkfs.xfs", part)
	err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
	if err == nil {
		defer os.Remove(TMP_MOUNT_DIR)
	} else {
		t.Fatal(err)
	}

	sudo("mount", part, TMP_MOUNT_DIR)
	defer sudo("umount", part)
	sudo("chmod", "a+rwx", TMP_MOUNT_DIR)
	err = ioutil.WriteFile(filepath.Join(TMP_MOUNT_DIR, "test"), []byte("OK"), 0666)
	if err != nil {
		t.Error("Can't write test file", err)
	}
	call(TMP_MOUNT_DIR, "--do")
	res, _, _ := cmd("df", "-BG", part)
	resLines := strings.Split(res, "\n")
	blocksStart := strings.Index(resLines[0], "1G-blocks")
	blocksEnd := blocksStart + len("1G-blocks")
	blocksString := strings.TrimSpace(resLines[1][blocksStart:blocksEnd])
	blocksString = strings.TrimSuffix(blocksString, "G")
	blocks, _ := parseUint(blocksString)
	if blocks != 100 {
		t.Errorf("%v[%v:%v]\n'%v'", resLines[1], blocksStart, blocksEnd, blocksString)
	}

	needPartitions := []testPartition{
		{1, GPT_START_BYTE, GPT_LAST_BYTE},
	}

	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
	}
	testBytes, err := ioutil.ReadFile(filepath.Join(TMP_MOUNT_DIR, "test"))
	if err != nil {
		t.Error("Can't read test file", err)
	}
	if string(testBytes) != "OK" {
		t.Error("Bad file content:", string(testBytes))
	}
}

func TestXfsPartitionUnmounted(t *testing.T) {
	disk, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(MSDOS_START_BYTE), s(MSDOS_START_BYTE+GB)) // 1Gb
	part := disk + "p1"
	sudo("mkfs.xfs", part)
	err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
	if err == nil {
		defer os.Remove(TMP_MOUNT_DIR)
	} else {
		t.Fatal(err)
	}

	sudo("mount", part, TMP_MOUNT_DIR)
	defer sudo("umount", part)
	sudo("chmod", "a+rwx", TMP_MOUNT_DIR)
	err = ioutil.WriteFile(filepath.Join(TMP_MOUNT_DIR, "test"), []byte("OK"), 0666)
	if err != nil {
		t.Error("Can't write test file", err)
	}
	sudo("umount", TMP_MOUNT_DIR)
	call(part, "--do")
	sudo("mount", part, TMP_MOUNT_DIR)
	res, _, _ := cmd("df", "-BG", part)
	resLines := strings.Split(res, "\n")
	blocksStart := strings.Index(resLines[0], "1G-blocks")
	blocksEnd := blocksStart + len("1G-blocks")
	blocksString := strings.TrimSpace(resLines[1][blocksStart:blocksEnd])
	blocksString = strings.TrimSuffix(blocksString, "G")
	blocks, _ := parseUint(blocksString)
	if blocks != 100 {
		t.Error(resLines[1])
	}

	needPartitions := []testPartition{
		{1, MSDOS_START_BYTE, MSDOS_LAST_BYTE},
	}
	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
	}
	testBytes, err := ioutil.ReadFile(filepath.Join(TMP_MOUNT_DIR, "test"))
	if err != nil {
		t.Error("Can't read test file", err)
	}
	if string(testBytes) != "OK" {
		t.Error("Bad file content:", string(testBytes))
	}
}

func TestLVMPartitionMSDOS(t *testing.T) {
	disk, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(MSDOS_START_BYTE), s(MSDOS_START_BYTE+GB)) // 1Gb
	sudo("parted", "-s", disk, "set", "1", "lvm", "on")

	part := disk + "p1"
	sudo("pvcreate", part)
	defer sudo("pvremove", part)
	sudo("vgcreate", LVM_VG_NAME, part)
	defer sudo("vgremove", "-f", LVM_VG_NAME)
	sudo("lvcreate", "-L", "500M", "-n", LVM_LV_NAME, LVM_VG_NAME)
	lvmLV := filepath.Join("/dev", LVM_VG_NAME, LVM_LV_NAME)
	defer sudo("lvremove", "-f", lvmLV)

	sudo("mkfs.xfs", lvmLV)

	err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
	if err == nil {
		defer os.Remove(TMP_MOUNT_DIR)
	} else {
		t.Fatal(err)
	}
	sudo("mount", lvmLV, TMP_MOUNT_DIR)
	defer sudo("umount", lvmLV)

	sudo("chmod", "a+rwx", TMP_MOUNT_DIR)
	err = ioutil.WriteFile(filepath.Join(TMP_MOUNT_DIR, "test"), []byte("OK"), 0666)
	if err != nil {
		t.Error("Can't write test file", err)
	}

	call(TMP_MOUNT_DIR, "--do")
	if 100 != df(TMP_MOUNT_DIR) {
		t.Error("Filesystem size")
	}

	needPartitions := []testPartition{
		{1, 32256, MSDOS_LAST_BYTE},
	}
	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
	}

	testBytes, err := ioutil.ReadFile(filepath.Join(TMP_MOUNT_DIR, "test"))
	if err != nil {
		t.Error("Can't read test file", err)
	}
	if string(testBytes) != "OK" {
		t.Error("Bad file content:", string(testBytes))
	}
}

func TestLVMPartitionGPT(t *testing.T) {
	disk, err := createTmpDevice("gpt")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(GPT_START_BYTE), s(GPT_START_BYTE+GB)) // 1Gb
	sudo("parted", "-s", disk, "set", "1", "lvm", "on")

	part := disk + "p1"
	sudo("pvcreate", part)
	defer sudo("pvremove", part)
	sudo("vgcreate", LVM_VG_NAME, part)
	defer sudo("vgremove", "-f", LVM_VG_NAME)
	sudo("lvcreate", "-L", "500M", "-n", LVM_LV_NAME, LVM_VG_NAME)
	lvmLV := filepath.Join("/dev", LVM_VG_NAME, LVM_LV_NAME)
	defer sudo("lvremove", "-f", lvmLV)

	sudo("mkfs.xfs", lvmLV)

	err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
	if err == nil {
		defer os.Remove(TMP_MOUNT_DIR)
	} else {
		t.Fatal(err)
	}
	sudo("mount", lvmLV, TMP_MOUNT_DIR)
	defer sudo("umount", lvmLV)

	sudo("chmod", "a+rwx", TMP_MOUNT_DIR)
	err = ioutil.WriteFile(filepath.Join(TMP_MOUNT_DIR, "test"), []byte("OK"), 0666)
	if err != nil {
		t.Error("Can't write test file", err)
	}

	call(TMP_MOUNT_DIR, "--do")
	if 100 != df(TMP_MOUNT_DIR) {
		t.Error("Filesystem size")
	}

	needPartitions := []testPartition{
		{1, GPT_START_BYTE, GPT_LAST_BYTE},
	}
	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
	}

	testBytes, err := ioutil.ReadFile(filepath.Join(TMP_MOUNT_DIR, "test"))
	if err != nil {
		t.Error("Can't read test file", err)
	}
	if string(testBytes) != "OK" {
		t.Error("Bad file content:", string(testBytes))
	}
}

func TestLVMPartitionADD_PV(t *testing.T) {
	disk, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(MSDOS_START_BYTE), s(GB-1)) // 1Gb
	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(GB), s(MSDOS_LAST_BYTE))    // Free PV
	sudo("parted", "-s", disk, "set", "1", "lvm", "on")

	part := disk + "p1"
	sudo("pvcreate", part)
	defer sudo("pvremove", part)
	sudo("pvcreate", disk+"p2") // create free pv
	defer sudo("pvremove", disk+"p2")
	sudo("vgcreate", LVM_VG_NAME, part)
	defer sudo("vgremove", "-f", LVM_VG_NAME)
	sudo("lvcreate", "-L", "500M", "-n", LVM_LV_NAME, LVM_VG_NAME)
	lvmLV := filepath.Join("/dev", LVM_VG_NAME, LVM_LV_NAME)
	defer sudo("lvremove", "-f", lvmLV)

	sudo("mkfs.xfs", lvmLV)

	err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
	if err == nil {
		defer os.Remove(TMP_MOUNT_DIR)
	} else {
		t.Fatal(err)
	}
	sudo("mount", lvmLV, TMP_MOUNT_DIR)
	defer sudo("umount", lvmLV)

	sudo("chmod", "a+rwx", TMP_MOUNT_DIR)
	err = ioutil.WriteFile(filepath.Join(TMP_MOUNT_DIR, "test"), []byte("OK"), 0666)
	if err != nil {
		t.Error("Can't write test file", err)
	}

	call(TMP_MOUNT_DIR, "--do")
	if 100 != df(TMP_MOUNT_DIR) {
		t.Error("Filesystem size", df(TMP_MOUNT_DIR))
	}

	needPartitions := []testPartition{
		{1, 32256, GB - 1},
		{2, GB, MSDOS_LAST_BYTE},
	}
	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
	}

	testBytes, err := ioutil.ReadFile(filepath.Join(TMP_MOUNT_DIR, "test"))
	if err != nil {
		t.Error("Can't read test file", err)
	}
	if string(testBytes) != "OK" {
		t.Error("Bad file content:", string(testBytes))
	}
}

func TestLVMPartition_ResizePV(t *testing.T) {
	disk, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(MSDOS_START_BYTE), s(MSDOS_START_BYTE+GB)) // 1Gb
	sudo("parted", "-s", disk, "set", "1", "lvm", "on")

	part := disk + "p1"
	sudo("pvcreate", part)
	defer sudo("pvremove", part)

	// Resize partition under PV and save old save of PV
	sudo("parted", "-s", disk, "rm", "1")
	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(MSDOS_START_BYTE), s(MSDOS_LAST_BYTE))
	sudo("parted", "-s", disk, "set", "1", "lvm", "on")
	sudo("blockdev", "--rereadpt", disk)

	sudo("vgcreate", LVM_VG_NAME, part)
	defer sudo("vgremove", "-f", LVM_VG_NAME)
	sudo("lvcreate", "-L", "500M", "-n", LVM_LV_NAME, LVM_VG_NAME)
	lvmLV := filepath.Join("/dev", LVM_VG_NAME, LVM_LV_NAME)
	defer sudo("lvremove", "-f", lvmLV)

	sudo("mkfs.xfs", lvmLV)

	err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
	if err == nil {
		defer os.Remove(TMP_MOUNT_DIR)
	} else {
		t.Fatal(err)
	}
	sudo("mount", lvmLV, TMP_MOUNT_DIR)
	defer sudo("umount", lvmLV)

	sudo("chmod", "a+rwx", TMP_MOUNT_DIR)
	err = ioutil.WriteFile(filepath.Join(TMP_MOUNT_DIR, "test"), []byte("OK"), 0666)
	if err != nil {
		t.Error("Can't write test file", err)
	}

	call(TMP_MOUNT_DIR, "--do")
	if 100 != df(TMP_MOUNT_DIR) {
		t.Error("Filesystem size", df(TMP_MOUNT_DIR))
	}

	needPartitions := []testPartition{
		{1, 32256, MSDOS_LAST_BYTE},
	}
	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
	}

	testBytes, err := ioutil.ReadFile(filepath.Join(TMP_MOUNT_DIR, "test"))
	if err != nil {
		t.Error("Can't read test file", err)
	}
	if string(testBytes) != "OK" {
		t.Error("Bad file content:", string(testBytes))
	}
}

func TestLVMPartitionInMiddleDiskMSDOS(t *testing.T) {
	disk, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(5*GB), s(6*GB))
	sudo("parted", "-s", disk, "set", "1", "lvm", "on")

	part := disk + "p1"
	sudo("pvcreate", part)
	defer sudo("pvremove", part)
	sudo("vgcreate", LVM_VG_NAME, part)
	defer sudo("vgremove", "-f", LVM_VG_NAME)
	sudo("lvcreate", "-L", "500M", "-n", LVM_LV_NAME, LVM_VG_NAME)
	lvmLV := filepath.Join("/dev", LVM_VG_NAME, LVM_LV_NAME)
	defer sudo("lvremove", "-f", lvmLV)

	sudo("mkfs.xfs", lvmLV)

	err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
	if err == nil {
		defer os.Remove(TMP_MOUNT_DIR)
	} else {
		t.Fatal(err)
	}
	sudo("mount", lvmLV, TMP_MOUNT_DIR)
	defer sudo("umount", lvmLV)

	sudo("chmod", "a+rwx", TMP_MOUNT_DIR)
	err = ioutil.WriteFile(filepath.Join(TMP_MOUNT_DIR, "test"), []byte("OK"), 0666)
	if err != nil {
		t.Error("Can't write test file", err)
	}

	call(TMP_MOUNT_DIR, "--do")
	if 100 != df(TMP_MOUNT_DIR) {
		t.Error("Filesystem size", df(TMP_MOUNT_DIR))
	}

	needPartitions := []testPartition{
		{2, MSDOS_START_BYTE, 5*GB - 1},
		{1, 5 * GB, MSDOS_LAST_BYTE},
	}

	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
		pretty.Println(readPartitions(disk))
	}

	testBytes, err := ioutil.ReadFile(filepath.Join(TMP_MOUNT_DIR, "test"))
	if err != nil {
		t.Error("Can't read test file", err)
	}
	if string(testBytes) != "OK" {
		t.Error("Bad file content:", string(testBytes))
	}
}

func TestLVMPartitionInMiddleDiskGPT(t *testing.T) {
	disk, err := createTmpDevice("gpt")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(5*GB), s(6*GB))
	sudo("parted", "-s", disk, "set", "1", "lvm", "on")

	part := disk + "p1"
	sudo("pvcreate", part)
	defer sudo("pvremove", part)

	sudo("vgcreate", LVM_VG_NAME, part)
	defer sudo("vgremove", "-f", LVM_VG_NAME)
	sudo("lvcreate", "-L", "500M", "-n", LVM_LV_NAME, LVM_VG_NAME)
	lvmLV := filepath.Join("/dev", LVM_VG_NAME, LVM_LV_NAME)
	defer sudo("lvremove", "-f", lvmLV)

	sudo("mkfs.xfs", lvmLV)

	err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
	if err == nil {
		defer os.Remove(TMP_MOUNT_DIR)
	} else {
		t.Fatal(err)
	}
	sudo("mount", lvmLV, TMP_MOUNT_DIR)
	defer sudo("umount", lvmLV)

	sudo("chmod", "a+rwx", TMP_MOUNT_DIR)
	err = ioutil.WriteFile(filepath.Join(TMP_MOUNT_DIR, "test"), []byte("OK"), 0666)
	if err != nil {
		t.Error("Can't write test file", err)
	}

	call(TMP_MOUNT_DIR, "--do")
	if 100 != df(TMP_MOUNT_DIR) {
		t.Error("Filesystem size", df(TMP_MOUNT_DIR))
	}

	needPartitions := []testPartition{
		{2, GPT_START_BYTE, 5*GB - 1},
		{1, 5 * GB, GPT_LAST_BYTE},
	}
	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
		pretty.Println(readPartitions(disk))
	}

	testBytes, err := ioutil.ReadFile(filepath.Join(TMP_MOUNT_DIR, "test"))
	if err != nil {
		t.Error("Can't read test file", err)
	}
	if string(testBytes) != "OK" {
		t.Error("Bad file content:", string(testBytes))
	}
}

func TestLVMPartitionIn2MiddleDiskMSDOS(t *testing.T) {
	disk, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(5*GB), s(6*GB-1))
	sudo("parted", "-s", disk, "set", "1", "lvm", "on")

	// No create lvm on second partition. Partition for split free space
	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(10*GB), s(11*GB-1))

	part := disk + "p1"
	sudo("pvcreate", part)
	defer sudo("pvremove", part)

	sudo("vgcreate", LVM_VG_NAME, part)
	defer sudo("vgremove", "-f", LVM_VG_NAME)
	sudo("lvcreate", "-L", "500M", "-n", LVM_LV_NAME, LVM_VG_NAME)
	lvmLV := filepath.Join("/dev", LVM_VG_NAME, LVM_LV_NAME)
	defer sudo("lvremove", "-f", lvmLV)

	sudo("mkfs.xfs", lvmLV)

	err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
	if err == nil {
		defer os.Remove(TMP_MOUNT_DIR)
	} else {
		t.Fatal(err)
	}
	sudo("mount", lvmLV, TMP_MOUNT_DIR)
	defer sudo("umount", lvmLV)

	sudo("chmod", "a+rwx", TMP_MOUNT_DIR)
	err = ioutil.WriteFile(filepath.Join(TMP_MOUNT_DIR, "test"), []byte("OK"), 0666)
	if err != nil {
		t.Error("Can't write test file", err)
	}

	call(TMP_MOUNT_DIR, "--do")
	if 99 != df(TMP_MOUNT_DIR) {
		t.Error("Filesystem size", df(TMP_MOUNT_DIR))
	}

	needPartitions := []testPartition{
		{3, MSDOS_START_BYTE, 5*GB - 1},
		{1, 5 * GB, 10*GB - 1},
		{2, 10 * GB, 11*GB - 1},
		{4, 11 * GB, MSDOS_LAST_BYTE},
	}
	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
		pretty.Println(readPartitions(disk))
	}

	testBytes, err := ioutil.ReadFile(filepath.Join(TMP_MOUNT_DIR, "test"))
	if err != nil {
		t.Error("Can't read test file", err)
	}
	if string(testBytes) != "OK" {
		t.Error("Bad file content:", string(testBytes))
	}
}

func TestLVMPartitionIn2MiddleDiskGPT(t *testing.T) {
	disk, err := createTmpDevice("gpt")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(5*GB), s(6*GB-1))
	sudo("parted", "-s", disk, "set", "1", "lvm", "on")

	// No create lvm on second partition. Partition for split free space
	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(10*GB), s(11*GB-1))

	part := disk + "p1"
	sudo("pvcreate", part)
	defer sudo("pvremove", part)
	sudo("vgcreate", LVM_VG_NAME, part)
	defer sudo("vgremove", "-f", LVM_VG_NAME)
	sudo("lvcreate", "-L", "500M", "-n", LVM_LV_NAME, LVM_VG_NAME)
	lvmLV := filepath.Join("/dev", LVM_VG_NAME, LVM_LV_NAME)
	defer sudo("lvremove", "-f", lvmLV)

	sudo("mkfs.xfs", lvmLV)

	err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
	if err == nil {
		defer os.Remove(TMP_MOUNT_DIR)
	} else {
		t.Fatal(err)
	}
	sudo("mount", lvmLV, TMP_MOUNT_DIR)
	defer sudo("umount", lvmLV)

	sudo("chmod", "a+rwx", TMP_MOUNT_DIR)
	err = ioutil.WriteFile(filepath.Join(TMP_MOUNT_DIR, "test"), []byte("OK"), 0666)
	if err != nil {
		t.Error("Can't write test file", err)
	}

	call(TMP_MOUNT_DIR, "--do")
	if 99 != df(TMP_MOUNT_DIR) {
		t.Error("Filesystem size", df(TMP_MOUNT_DIR))
	}

	needPartitions := []testPartition{
		{3, GPT_START_BYTE, 5*GB - 1},
		{1, 5 * GB, 10*GB - 1},
		{2, 10 * GB, 11*GB - 1},
		{4, 11 * GB, GPT_LAST_BYTE},
	}
	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
		pretty.Println(readPartitions(disk))
	}

	testBytes, err := ioutil.ReadFile(filepath.Join(TMP_MOUNT_DIR, "test"))
	if err != nil {
		t.Error("Can't read test file", err)
	}
	if string(testBytes) != "OK" {
		t.Error("Bad file content:", string(testBytes))
	}
}

func TestLVMPartitionWithoutFS(t *testing.T) {
	disk, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(MSDOS_START_BYTE), s(MSDOS_START_BYTE+GB-1)) // 1Gb
	sudo("parted", "-s", disk, "set", "1", "lvm", "on")

	part := disk + "p1"
	sudo("pvcreate", part)
	defer sudo("pvremove", part)
	sudo("vgcreate", LVM_VG_NAME, part)
	defer sudo("vgremove", "-f", LVM_VG_NAME)
	sudo("lvcreate", "-L", "500M", "-n", LVM_LV_NAME, LVM_VG_NAME)
	lvmLV := filepath.Join("/dev", LVM_VG_NAME, LVM_LV_NAME)
	defer sudo("lvremove", "-f", lvmLV)

	call(lvmLV, "--do")

	needPartitions := []testPartition{
		{1, 32256, MSDOS_LAST_BYTE},
	}
	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
	}
	lvmLVSize := lvmLVGetSize(LVM_VG_NAME + "/" + LVM_LV_NAME)
	PartitionSize := uint64(MSDOS_LAST_BYTE - MSDOS_START_BYTE + 1)
	MinSize := PartitionSize - 100*1024*1024
	if lvmLVSize < MinSize || lvmLVSize > PartitionSize {
		t.Error("LVM Size:", formatSize(lvmLVSize), lvmLVSize)
	}
}

func TestRecursiveHierarchy(t *testing.T) {
	disk, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(MSDOS_START_BYTE), s(MSDOS_LAST_BYTE))

	part := disk + "p1"
	sudo("pvcreate", part)
	defer sudo("pvremove", part)
	sudo("vgcreate", LVM_VG_NAME, part)
	defer sudo("vgremove", "-f", LVM_VG_NAME)
	sudo("lvcreate", "-L", "500M", "-n", LVM_LV_NAME, LVM_VG_NAME)
	lvmLV := filepath.Join("/dev", LVM_VG_NAME, LVM_LV_NAME)
	defer sudo("lvremove", "-f", lvmLV)

	// Create pv in LVM
	sudo("pvcreate", lvmLV)
	defer sudo("lvremove", lvmLV)

	// Extend VG to PV in the VM - create recursive dependency
	sudo("vgextend", LVM_VG_NAME, lvmLV)
	defer sudo("vgreduce", LVM_VG_NAME, lvmLV)

	resetProgramState()
	_, err = extendScanWays(lvmLV)
	if err == nil {
		t.Error("MUST detect hierarchy recursive error")
	} else {
		t.Log("Recursive error OK detected as:", err)
	}
}

func TestLVMPartition_LimitFilterForAlreadyPlacedOneDevice(t *testing.T) {
	disk, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	disk2, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk2)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(MSDOS_START_BYTE), s(MSDOS_START_BYTE+GB)) // 1Gb
	sudo("parted", "-s", disk, "set", "1", "lvm", "on")

	part := disk + "p1"
	sudo("pvcreate", part)
	defer sudo("pvremove", part)
	sudo("vgcreate", LVM_VG_NAME, part)
	defer sudo("vgremove", "-f", LVM_VG_NAME)
	sudo("lvcreate", "-L", "500M", "-n", LVM_LV_NAME, LVM_VG_NAME)
	lvmLV := filepath.Join("/dev", LVM_VG_NAME, LVM_LV_NAME)
	defer sudo("lvremove", "-f", lvmLV)

	sudo("mkfs.xfs", lvmLV)

	err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
	if err == nil {
		defer os.Remove(TMP_MOUNT_DIR)
	} else {
		t.Fatal(err)
	}
	sudo("mount", lvmLV, TMP_MOUNT_DIR)
	defer sudo("umount", lvmLV)

	sudo("chmod", "a+rwx", TMP_MOUNT_DIR)
	err = ioutil.WriteFile(filepath.Join(TMP_MOUNT_DIR, "test"), []byte("OK"), 0666)
	if err != nil {
		t.Error("Can't write test file", err)
	}

	call("--filter=LVM_ALREADY_PLACED", TMP_MOUNT_DIR, "--do")
	if 100 != df(TMP_MOUNT_DIR) {
		t.Error("Filesystem size", df(TMP_MOUNT_DIR))
	}

	needPartitions := []testPartition{
		{1, 32256, MSDOS_LAST_BYTE},
	}
	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
	}

	if len(readPartitions(disk2)) != 0 {
		t.Error(pretty.Sprintf("%v", readPartitions(disk2)))
	}

	testBytes, err := ioutil.ReadFile(filepath.Join(TMP_MOUNT_DIR, "test"))
	if err != nil {
		t.Error("Can't read test file", err)
	}
	if string(testBytes) != "OK" {
		t.Error("Bad file content:", string(testBytes))
	}
}

func TestLVMPartition_LimitFilterForAlreadyPlacedTwoDevices(t *testing.T) {
	disk, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	disk2, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk2)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(MSDOS_START_BYTE), s(MSDOS_START_BYTE+GB)) // 1Gb
	sudo("parted", "-s", disk, "set", "1", "lvm", "on")
	sudo("parted", "-s", disk2, "unit", "b", "mkpart", "primary", s(MSDOS_START_BYTE), s(MSDOS_START_BYTE+GB)) // 1Gb
	sudo("parted", "-s", disk2, "set", "1", "lvm", "on")

	part := disk + "p1"
	part2 := disk2 + "p1"

	sudo("pvcreate", part)
	sudo("pvcreate", part2)
	defer sudo("pvremove", part)
	defer sudo("pvremove", part2)

	sudo("vgcreate", LVM_VG_NAME, part, part2)
	defer sudo("vgremove", "-f", LVM_VG_NAME)
	sudo("lvcreate", "-L", "500M", "-n", LVM_LV_NAME, LVM_VG_NAME)
	lvmLV := filepath.Join("/dev", LVM_VG_NAME, LVM_LV_NAME)
	defer sudo("lvremove", "-f", lvmLV)

	sudo("mkfs.xfs", lvmLV)

	err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
	if err == nil {
		defer os.Remove(TMP_MOUNT_DIR)
	} else {
		t.Fatal(err)
	}
	sudo("mount", lvmLV, TMP_MOUNT_DIR)
	defer sudo("umount", lvmLV)

	sudo("chmod", "a+rwx", TMP_MOUNT_DIR)
	err = ioutil.WriteFile(filepath.Join(TMP_MOUNT_DIR, "test"), []byte("OK"), 0666)
	if err != nil {
		t.Error("Can't write test file", err)
	}

	call("--filter=LVM_ALREADY_PLACED", TMP_MOUNT_DIR, "--do")
	if 200 != df(TMP_MOUNT_DIR) {
		t.Error("Filesystem size", df(TMP_MOUNT_DIR))
	}

	needPartitions := []testPartition{
		{1, 32256, MSDOS_LAST_BYTE},
	}
	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
	}

	// Same layout for disk 2
	partDiff = pretty.Diff(readPartitions(disk2), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
	}

	testBytes, err := ioutil.ReadFile(filepath.Join(TMP_MOUNT_DIR, "test"))
	if err != nil {
		t.Error("Can't read test file", err)
	}
	if string(testBytes) != "OK" {
		t.Error("Bad file content:", string(testBytes))
	}
}

func TestLVMPartition_LimitFilterOneDeviceWhilePlacedInTwo(t *testing.T) {
	disk, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	disk2, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk2)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(MSDOS_START_BYTE), s(MSDOS_START_BYTE+GB-1)) // 1Gb
	sudo("parted", "-s", disk, "set", "1", "lvm", "on")
	sudo("parted", "-s", disk2, "unit", "b", "mkpart", "primary", s(MSDOS_START_BYTE), s(MSDOS_START_BYTE+GB-1)) // 1Gb
	sudo("parted", "-s", disk2, "set", "1", "lvm", "on")

	part := disk + "p1"
	part2 := disk2 + "p1"

	sudo("pvcreate", part)
	sudo("pvcreate", part2)
	defer sudo("pvremove", part)
	defer sudo("pvremove", part2)

	sudo("vgcreate", LVM_VG_NAME, part)
	defer sudo("vgremove", "-f", LVM_VG_NAME)
	sudo("lvcreate", "-L", "500M", "-n", LVM_LV_NAME, LVM_VG_NAME)
	lvmLV := filepath.Join("/dev", LVM_VG_NAME, LVM_LV_NAME)
	defer sudo("lvremove", "-f", lvmLV)

	sudo("vgextend", LVM_VG_NAME, part2)

	sudo("mkfs.xfs", lvmLV)

	err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
	if err == nil {
		defer os.Remove(TMP_MOUNT_DIR)
	} else {
		t.Fatal(err)
	}
	sudo("mount", lvmLV, TMP_MOUNT_DIR)
	defer sudo("umount", lvmLV)

	sudo("chmod", "a+rwx", TMP_MOUNT_DIR)
	err = ioutil.WriteFile(filepath.Join(TMP_MOUNT_DIR, "test"), []byte("OK"), 0666)
	if err != nil {
		t.Error("Can't write test file", err)
	}

	call("--filter="+disk, TMP_MOUNT_DIR, "--do") // filter for one disk only
	if 101 != df(TMP_MOUNT_DIR) {                 // 100 from first disk + 1 from pv existed in second disk
		t.Error("Filesystem size", df(TMP_MOUNT_DIR))
	}

	needPartitions := []testPartition{
		{1, MSDOS_START_BYTE, MSDOS_LAST_BYTE},
	}
	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
	}

	needPartitions2 := []testPartition{
		{1, MSDOS_START_BYTE, MSDOS_START_BYTE + GB - 1},
	}
	partDiff = pretty.Diff(readPartitions(disk2), needPartitions2)
	if partDiff != nil {
		t.Error(partDiff)
	}

	testBytes, err := ioutil.ReadFile(filepath.Join(TMP_MOUNT_DIR, "test"))
	if err != nil {
		t.Error("Can't read test file", err)
	}
	if string(testBytes) != "OK" {
		t.Error("Bad file content:", string(testBytes))
	}
}

func TestLVMPartition_LVMInOneDiskFilterIsNullAndtwoDisksForExtend(t *testing.T) {
	disk, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	disk2, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk2)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(MSDOS_START_BYTE), s(MSDOS_START_BYTE+GB)) // 1Gb
	sudo("parted", "-s", disk, "set", "1", "lvm", "on")

	part := disk + "p1"
	sudo("pvcreate", part)
	defer sudo("pvremove", part)
	sudo("vgcreate", LVM_VG_NAME, part)
	defer sudo("vgremove", "-f", LVM_VG_NAME)
	sudo("lvcreate", "-L", "500M", "-n", LVM_LV_NAME, LVM_VG_NAME)
	lvmLV := filepath.Join("/dev", LVM_VG_NAME, LVM_LV_NAME)
	defer sudo("lvremove", "-f", lvmLV)

	sudo("mkfs.xfs", lvmLV)

	err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
	if err == nil {
		defer os.Remove(TMP_MOUNT_DIR)
	} else {
		t.Fatal(err)
	}
	sudo("mount", lvmLV, TMP_MOUNT_DIR)
	defer sudo("umount", lvmLV)

	sudo("chmod", "a+rwx", TMP_MOUNT_DIR)
	err = ioutil.WriteFile(filepath.Join(TMP_MOUNT_DIR, "test"), []byte("OK"), 0666)
	if err != nil {
		t.Error("Can't write test file", err)
	}

	call("--filter=", TMP_MOUNT_DIR, "--do")
	if 200 != df(TMP_MOUNT_DIR) {
		t.Error("Filesystem size", df(TMP_MOUNT_DIR))
	}

	needPartitions := []testPartition{
		{1, 32256, MSDOS_LAST_BYTE},
	}
	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
	}

	partDiff = pretty.Diff(readPartitions(disk2), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
	}

	testBytes, err := ioutil.ReadFile(filepath.Join(TMP_MOUNT_DIR, "test"))
	if err != nil {
		t.Error("Can't read test file", err)
	}
	if string(testBytes) != "OK" {
		t.Error("Bad file content:", string(testBytes))
	}
}

func TestIssue13_ReadExtendedPartitions(t *testing.T) {
	disk, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	sudo("parted", "-s", disk, "mkpart", "extended", "0%", "100%")
	defer sudo("parted", "-s", disk, "rm", "1")
	sudo("parted", "-s", disk, "mkpart", "logical", "1G", "2G")
	defer sudo("parted", "-s", disk, "rm", "5")
	part := disk + "p5"
	sudo("mkfs.xfs", part)

	// in issue failed by panic
	call("--do", part)
}

// https://github.com/rekby/fsextender/issues/14
func TestIssue14_ExtractPartNumberFromLinks(t *testing.T) {
	disk, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)
	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", s(MSDOS_START_BYTE), s(GB-1))
	defer sudo("parted", "-s", disk, "rm", "1")

	part := disk + "p1"
	sudo("mkfs.xfs", disk+"p1")
	sudo("xfs_admin", "-U", "78c1656c-a17f-11e5-b076-74e6e20dc9f0", part)

	for i := 0; i < TRY_COUNT; i++ {
		_, err = os.Stat("/dev/disk/by-uuid/78c1656c-a17f-11e5-b076-74e6e20dc9f0")
		if os.IsNotExist(err) {
			if i < TRY_COUNT-1 {
				time.Sleep(time.Second)
			}
		} else {
			break
		}
	}
	if err != nil {
		t.Fatal("Can't create symlink")
	}

	err = os.MkdirAll(TMP_MOUNT_DIR, 0600)
	if err != nil {
		t.Fatal("Can't create tmp mount dir")
	}
	defer os.Remove(TMP_MOUNT_DIR)

	sudo("mount", "/dev/disk/by-uuid/78c1656c-a17f-11e5-b076-74e6e20dc9f0", TMP_MOUNT_DIR)
	defer sudo("umount", TMP_MOUNT_DIR)

	call("--do", TMP_MOUNT_DIR)

	needPartitions := []testPartition{
		{1, 32256, MSDOS_LAST_BYTE},
	}
	partDiff := pretty.Diff(readPartitions(disk), needPartitions)
	if partDiff != nil {
		t.Error(partDiff)
	}
}

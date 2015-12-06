package main

import (
	"fmt"
	"github.com/rekby/pretty"
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"
)

const GB = 1024 * 1024 * 1024
const TMP_DIR = "/tmp"
const TMP_MOUNT_DIR = "/tmp/fsextender-test-mount-dir"

var PART_TABLES = []string{"msdos", "gpt"}

type testPartition struct {
	Number uint64
	Start  uint64
	Last   uint64
}

// Call main program
func call(args ...string) {
	sudo("./fsextender", args...)
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
	_, err = f.WriteAt([]byte{0}, 100*GB)
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

	cmd("sudo", "losetup", "-d", path)
	os.Remove(filePath)
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

func sudo(command string, args ...string) (res string, errString string, err error) {
	args = append([]string{command}, args...)
	return cmd("sudo", args...)
}

func TestExt4Partition(t *testing.T) {
	for _, parttable := range PART_TABLES {
		func() {
			disk, err := createTmpDevice(parttable)
			if err != nil {
				t.Fatal(err)
			}
			defer deleteTmpDevice(disk)

			sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", "32256", "1073774080") // 1Gb
			part := disk + "p1"
			sudo("mkfs.ext4", part)
			err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
			if err == nil {
				defer os.Remove(TMP_MOUNT_DIR)
			} else {
				t.Fatal(parttable, err)
			}

			sudo("mount", part, TMP_MOUNT_DIR)
			defer sudo("umount", part)
			call(TMP_MOUNT_DIR, "--do")
			res, _, _ := cmd("df", "-BG", part)
			resLines := strings.Split(res, "\n")
			blocksStart := strings.Index(resLines[0], "1G-blocks")
			blocksEnd := blocksStart + len("1G-blocks")
			blocksString := strings.TrimSpace(resLines[1][blocksStart:blocksEnd])
			blocksString = strings.TrimSuffix(blocksString, "G")
			blocks, _ := parseUint(blocksString)
			if blocks != 99 {
				t.Error(parttable, resLines[1])
			}

			var needPartitions []testPartition
			switch parttable {
			case "gpt":
				needPartitions = []testPartition{
					{1, 32256, 107374165503},
				}
			case "msdos":
				needPartitions = []testPartition{
					{1, 32256, 107374182399},
				}
			default:
				t.Fatal()
			}

			partDiff := pretty.Diff(readPartitions(disk), needPartitions)
			if partDiff != nil {
				t.Error(parttable, partDiff)
			}
		}()
	}
}

func TestXfsPartition(t *testing.T) {
	for _, parttable := range PART_TABLES {
		func() {
			disk, err := createTmpDevice(parttable)
			if err != nil {
				t.Fatal(err)
			}
			defer deleteTmpDevice(disk)

			sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", "32256", "1073774080") // 1Gb
			part := disk + "p1"
			sudo("mkfs.xfs", part)
			err = os.MkdirAll(TMP_MOUNT_DIR, 0700)
			if err == nil {
				defer os.Remove(TMP_MOUNT_DIR)
			} else {
				t.Fatal(parttable, err)
			}

			sudo("mount", part, TMP_MOUNT_DIR)
			defer sudo("umount", part)
			call(TMP_MOUNT_DIR, "--do")
			res, _, _ := cmd("df", "-BG", part)
			resLines := strings.Split(res, "\n")
			blocksStart := strings.Index(resLines[0], "1G-blocks")
			blocksEnd := blocksStart + len("1G-blocks")
			blocksString := strings.TrimSpace(resLines[1][blocksStart:blocksEnd])
			blocksString = strings.TrimSuffix(blocksString, "G")
			blocks, _ := parseUint(blocksString)
			if blocks != 100 {
				t.Error(parttable, resLines[1])
			}

			var needPartitions []testPartition
			switch parttable {
			case "gpt":
				needPartitions = []testPartition{
					{1, 32256, 107374165503},
				}
			case "msdos":
				needPartitions = []testPartition{
					{1, 32256, 107374182399},
				}
			default:
				t.Fatal()
			}

			partDiff := pretty.Diff(readPartitions(disk), needPartitions)
			if partDiff != nil {
				t.Error(parttable, partDiff)
			}
		}()
	}
}

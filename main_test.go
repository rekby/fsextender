package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"
)

const GB = 1024 * 1024 * 1024
const TMP_DIR = "/tmp"

var PART_TABLES = []string{"msdos", "gpt"}

type testPartition struct {
	number uint64
	start  uint64
	last   uint64
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
	os.Remove(path)
}

func readPartitions(path string) (res []testPartition) {
	lines := cmdTrimLines("sudo", "parted", "-s", path, "unit", "b", "print")

	var numStart, numFinish, startStart, startFinish, endStart, endFinish int
	var startParse = false
	for _, line := range lines {
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
		partition.number, _ = parseUint(strings.TrimSpace(line[numStart:numFinish]))
		if partition.number == 0 {
			continue
		}
		partition.start, _ = parseUint(strings.TrimSpace(line[startStart:startFinish]))
		partition.last, _ = parseUint(strings.TrimSpace(line[endStart:endFinish]))
		res = append(res, partition)
	}
	return
}

func sudo(command string, args ...string) (res string, errString string, err error) {
	args = append([]string{command}, args...)
	return cmd("sudo", args...)
}

func TestExt4Partition(t *testing.T) {
	disk, err := createTmpDevice("msdos")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteTmpDevice(disk)

	sudo("parted", "-s", disk, "unit", "b", "mkpart", "primary", "32256", "1073774080") // 1Gb

}

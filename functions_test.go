package fsextender

import (
	"bytes"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestDocumentationActual(t *testing.T) {
	var err error
	var builtinBytes, originalBytes []byte

	// Usage
	originalBytes, err = ioutil.ReadFile("usage.txt")
	if err != nil {
		t.Error(err)
	}
	builtinBytes, err = usageTxtBytes()
	if err != nil {
		t.Error(err)
	}
	if !bytes.Equal(originalBytes, builtinBytes) {
		t.Error("Usage actual")
	}

	// Readme
	originalBytes, err = ioutil.ReadFile("README.md")
	if err != nil {
		t.Error(err)
	}
	builtinBytes, err = readmeMdBytes()
	if err != nil {
		t.Error(err)
	}
	if !bytes.Equal(originalBytes, builtinBytes) {
		t.Error("Readme actual")
	}
}

func TestExpandFilter(t *testing.T) {
	if expandFilter(nil, "") != "" {
		t.Error(expandFilter(nil, ""))
	}
	if expandFilter(nil, ".*") != ".*" {
		t.Error(expandFilter(nil, ".*"))
	}

	// fullpath
	if expandFilter(nil, "/dev/loop.*") != "^/dev/loop.*" {
		t.Error(expandFilter(nil, "/dev/loop.*"))
	}

	// diskname
	if expandFilter(nil, "loop") != "loop[^/]*$" {
		t.Error(expandFilter(nil, "loop"))
	}

	// diskpath
	if expandFilter(nil, "/dev/loop") != "^/dev/loop[^/]*$" {
		t.Error(expandFilter(nil, "/dev/loop"))
	}

	var storage = []storageItem{
		{
			Type:   type_FS, // # 0
			FSType: "xfs",
			Path:   "/dev/storage/test",
			Child:  -1,
		},
		{
			Type:  type_LVM_LV, // #1
			Path:  "/dev/storage/test",
			Child: 0,
		},
		{
			Type:  type_LVM_GROUP, // #2
			Path:  "storage",
			Child: 1,
		},
		{
			Type:  type_LVM_PV, // #3
			Path:  "/dev/sda1",
			Child: 2,
		},
		{
			Type:  type_LVM_PV, // #4
			Path:  "/dev/sda2",
			Child: 2,
		},
		{
			Type:  type_LVM_PV, // #5
			Path:  "/dev/sdb1",
			Child: 2,
		},
		{
			Type:  type_LVM_PV_ADD, // #6
			Path:  "/dev/sdc1",
			Child: 2,
		},
		{
			Type:  type_LVM_PV_NEW, // #7
			Path:  "/dev/sdd1",
			Child: 2,
		},
		{
			Type:  type_PARTITION_NEW, // #8
			Path:  "/dev/sde1",
			Child: 7,
		},
	}

	if "^/dev/sda[^/]*$|^/dev/sdb[^/]*" == expandFilter(storage, "/dev/sda,/dev/sdb") {
		t.Error(expandFilter(storage, "/dev/sda,/dev/sdb"))
	}

	if "^/dev/sda[^/]*$|^/dev/sdb[^/]*$" != expandFilter(storage, FILTER_LVM_ALREADY_PLACED) {
		t.Error(expandFilter(storage, FILTER_LVM_ALREADY_PLACED))
	}

	if "^/dev/loop[^/]*$|^/dev/sda[^/]*$|^/dev/sdb[^/]*$" != expandFilter(storage, "LVM_ALREADY_PLACED,/dev/loop") {
		t.Error(expandFilter(storage, "LVM_ALREADY_PLACED,/dev/loop"))
	}
}

func TestItemTypeToString(t *testing.T) {
	for i := type_UNKNOWN; i <= type_LAST; i++ {
		if !strings.HasPrefix(i.String(), "type_") {
			t.Error(i.String())
		}
	}
	if !strings.HasPrefix(storageItemType(-1).String(), "storageItemType(") {
		t.Error(storageItemType(-1).String())
	}
	if !strings.HasPrefix(storageItemType(type_LAST+1).String(), "storageItemType(") {
		t.Error(storageItemType(type_LAST + 1).String())
	}
}

func TestReadLink(t *testing.T) {
	var dir, res string
	var err error

	dir, err = ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	fn := func(f string) string {
		return filepath.Join(dir, f)
	}

	log.SetOutput(&bytes.Buffer{})
	res, err = readLink(fn("new"))
	log.SetOutput(os.Stderr)

	if res != fn("new") || !os.IsNotExist(err) {
		t.Error(res, err)
	}

	ioutil.WriteFile(fn("0"), []byte{}, 0600)
	res, err = readLink(fn("0"))
	if res != fn("0") || err != nil {
		t.Error(res, err)
	}

	os.Symlink(fn("0"), fn("1"))
	res, err = readLink(fn("1"))
	if res != fn("0") || err != nil {
		t.Error(res, err)
	}
}

func TestSortPartitionsByFirstByte(t *testing.T) {
	var arr partitionSortByFirstByte

	test := func() bool {
		for i := 1; i < len(arr); i++ {
			if arr[i-1].FirstByte > arr[i].FirstByte {
				return false
			}
		}
		return true
	}

	arr = nil
	arr = append(arr, partition{FirstByte: 0}, partition{FirstByte: 1}, partition{FirstByte: 3})
	sort.Sort(arr)
	if !test() {
		t.Error()
	}

	arr = nil
	arr = append(arr, partition{FirstByte: 3}, partition{FirstByte: 2}, partition{FirstByte: 1})
	sort.Sort(arr)
	if !test() {
		t.Error()
	}

	arr = nil
	arr = append(arr, partition{FirstByte: 3}, partition{FirstByte: 1}, partition{FirstByte: 2})
	sort.Sort(arr)
	if !test() {
		t.Error()
	}

	arr = nil
	sort.Sort(arr)
	if !test() {
		t.Error()
	}

	arr = nil
	arr = append(arr, partition{FirstByte: 2})
	sort.Sort(arr)
	if !test() {
		t.Error()
	}
}

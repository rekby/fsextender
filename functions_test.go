package main

import (
	"bytes"
	"io/ioutil"
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

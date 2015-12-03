package main

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/rekby/gpt"
	"github.com/rekby/mbr"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

//go:generate stringer -type storageItemType
type storageItemType int

// Max depth of storage. Used for infinite loop detection.
// Максимальная глубина стека устройств. В настроящий момент используется как простой определитель циклов
const max_STORAGE_DEEP = 1000

// Minimum size of created partition. For avoid create new partition with few KB of space.
// Минимальный размер свободного места для создания нового раздела
const min_SIZE_NEW_PARTITION = 100 * 1024 * 1024

// Count of extents for PV metadata (for calculations).
// Количество блоков LVM PV, резервируемых под метаданные (при расчетах).
const lvm_PV_METADATA_RESERVED = 2

const (
	type_UNKNOWN storageItemType = iota
	type_FS
	type_DISK
	type_LVM_GROUP
	type_LVM_PV

	// Unused LVM PV
	// Неиспользуемый LVM PV
	type_LVM_PV_ADD

	// new LVM PV
	// Новый физический раздел LVM
	type_LVM_PV_NEW
	type_LVM_LV
	type_PARTITION
	type_PARTITION_NEW
)

type storageItem struct {
	Type      storageItemType // Storage type. Тип устройства
	Path      string          // Path to device or name of device (for LVM group). Путь к устройству или имя (например для LVM)
	Child     int             // Index of storage, what can help extend the item. Индекс устройства, на который будет влиять расширение этого устройства
	Size      uint64
	FreeSpace uint64 // Free space of item, without sum of unde items. For example - size for PV extend to size of partition
	// or free space in LVM Volume group.
	// Максимальный объем, который может предоставить устройство, без учета роста нижележащих устройст
	// Например расширение PV до размера раздела или расширение раздела до размера диска, свободное место в LVM Group и т.п.
	FSType        string    // Type of file system (for type type_FS) тип файловой системы (для типа type_FS)
	Partition     partition // For types type_PARTITION and type_PARTITION_NEW. Описание раздела диска - для типов (type_PARTITION, type_PARTITION_NEW)
	LVMExtentSize uint64    // Extent size for type_LVM_GROUP, type_LVM_PV, type_LVM_PV_ADD, type_LVM_PV_NEW. Размер экстента для типа type_LVM_GROUP, type_LVM_PV, type_LVM_PV_ADD, type_LVM_PV_NEW
}

func (this storageItem) String() string {
	base := fmt.Sprintf("[Type: %v, Path: %v, Size: %v (+%v), Child: %v",
		this.Type, this.Path, formatSize(this.Size), formatSize(this.FreeSpace), this.Child)
	switch this.Type {
	case type_FS:
		base += ", FS: " + this.FSType
	case type_PARTITION, type_PARTITION_NEW:
		base += ", PartNum=" + strconv.FormatUint(uint64(this.Partition.Number), 10)
	case type_LVM_GROUP, type_LVM_PV, type_LVM_PV_ADD, type_LVM_PV_NEW:
		base += ", ExtentSize: " + formatSize(this.LVMExtentSize)
	}
	return base + "]"
}

type diskInfo struct {
	Path              string
	PartTable         string // msdos/gpt
	Size              uint64 // Bytes
	Major             int
	Minor             int
	SectorSizeLogical uint64 // Logical size of sector - for operation with partition table (in bytes). Логический размер сектора диска, в байтах
	Partitions        []partition
	MaxPartitionCount uint32
}

type partition struct {
	Disk      *diskInfo
	Path      string
	Number    uint32 // Partition numbers start start from 1. Value 0 mean free space
	FirstByte uint64
	LastByte  uint64
}
type partitionSortByFirstByte []partition

// implement sort.Interface
func (arr partitionSortByFirstByte) Len() int {
	return len(arr)
}
func (arr partitionSortByFirstByte) Less(i, j int) bool {
	return arr[i].FirstByte < arr[j].FirstByte
}
func (arr partitionSortByFirstByte) Swap(i, j int) {
	tmpSwap := arr[i]
	arr[i] = arr[j]
	arr[j] = tmpSwap
}

func (p partition) Size() uint64 {
	return p.LastByte - p.FirstByte + 1
}
func (p partition) IsFreeSpace() bool {
	return p.Number == 0
}
func (p partition) makePath() string {
	// Drive path ends with number, for example /dev/loop0
	if len(p.Disk.Path) > 0 {
		last := p.Disk.Path[len(p.Disk.Path)-1]
		if last >= '0' && last <= '9' {
			return p.Disk.Path + "p" + strconv.FormatUint(uint64(p.Number), 10)
		}
	}
	return p.Disk.Path + strconv.FormatUint(uint64(p.Number), 10)
}

type lvmPV struct {
	Path        string
	VolumeGroup string
	Size        uint64
}

var majorMinorDeviceTypeCache = make(map[[2]int]storageItem)

func blkid(path string) string {
	buf := &bytes.Buffer{}
	cmd := exec.Command("blkid", path)
	cmd.Stdout = buf
	cmd.Run()
	out := buf.Bytes()
	start := bytes.Index(out, []byte(`TYPE="`))
	if start == -1 {
		return ""
	}
	typeBytes := out[start+len(`TYPE="`):]
	end := bytes.Index(typeBytes, []byte(`"`))
	if end == -1 {
		return ""
	}

	return string(typeBytes[:end])
}

var diskNewPartitionNumLastGeneratedNum = make(map[[2]int]uint32)

func diskNewPartitionNum(disk diskInfo) uint32 {
	mm := [2]int{disk.Major, disk.Minor}
	start := diskNewPartitionNumLastGeneratedNum[mm]
partNumLoop:
	for partNum := start + 1; true; partNum++ {
		for _, part := range disk.Partitions {
			// Check if exist partition with the number
			// Проверяем есть ли разделы с таким номером
			if part.Number == partNum {
				continue partNumLoop
			}
		}
		diskNewPartitionNumLastGeneratedNum[mm] = partNum
		return partNum
	}
	return 0
}

func extendScanWays(startPoint string) (storage []storageItem, err error) {
	startPoint = filepath.Clean(startPoint)
	scanLVM()

	// Check if startPoint is mount point of file system. If yes - find mounted device. Take last mount line.
	// проверяем является ли startPoint точкой монтирования. Если да - находим смонтированное устройство.
	// Если подходящих строк монтирования несколько - берём последнюю строку.
	mountsBytes, err := ioutil.ReadFile("/proc/mounts")
	if err != nil {
		return
	}
	mountFrom := ""
	for _, lineBytes := range bytes.Split(mountsBytes, []byte("\n")) {
		parts := bytes.Split(lineBytes, []byte(" "))
		if len(parts) < 3 {
			// empty line
			continue
		}
		from := parts[0]
		to := parts[1]
		if string(to) == startPoint {
			mountFrom = string(from)
		}
	}
	if mountFrom != "" {
		startPoint = mountFrom
	}
	storage = make([]storageItem, 0)

	toScan := []storageItem{storageItem{Path: startPoint, Child: -1}}
toScanLoop:
	for len(toScan) > 0 {
		if len(storage) > max_STORAGE_DEEP {
			return storage, errors.New("Struct is cicle or very large")
		}
		// pop item
		item := toScan[len(toScan)-1]
		toScan = toScan[:len(toScan)-1]

		switch item.Type {
		case type_UNKNOWN:
			blk := blkid(item.Path)
			major, minor := getMajorMinor(item.Path)
			switch {
			case blk == "ext2", blk == "ext3", blk == "ext4", blk == "xfs":
				item.Type = type_FS
				item.FSType = blk
			case getTypeByMajorMinor(major, minor) != type_UNKNOWN:
				item.Type = getTypeByMajorMinor(major, minor)
			default:
				// Skip unknown devices
				// не получилось понять что за устройство - пропускаем
				log.Printf("Can't detect device type. Path: '%v' Blk: '%v', major: %v, minor: %v", item.Path, blk, major, minor)
				continue toScanLoop
			}
			// Scan once more with right type of device
			// тип устройства определился - будем сканировать подробнее на следующем проходе
			toScan = append(toScan, item)

		case type_FS:
			switch item.FSType {
			case "ext2", "ext3", "ext4":
				item.Size, err = fsGetSizeExt(item.Path)
				if err != nil {
					log.Printf("Can't get size of filesystem: %v (%v). Skip it.\n", item.Path, err)
					continue toScanLoop
				}
			case "xfs":
				item.Size, err = fsGetSizeXFS(item.Path)
				if err != nil {
					log.Printf("Can't get size of filesystem: %v (%v). Skip it.\n", item.Path, err)
					continue toScanLoop
				}
			default:
				log.Printf("I don't khow method to detect size of filesystem %v (%v). Skip it.", item.Path, item.FSType)
				continue toScanLoop
			}

			storage = append(storage, item)
			major, minor := getMajorMinor(item.Path)
			underLevelType := getTypeByMajorMinor(major, minor)
			if underLevelType != type_UNKNOWN {
				newItem := storageItem{
					Path:  item.Path,
					Type:  underLevelType,
					Child: len(storage) - 1,
				}
				toScan = append(toScan, newItem)
			}

		case type_PARTITION:
			diskPath, partNumber, err := extractPartNumber(item.Path)
			if err != nil {
				log.Println(err.Error())
				continue toScanLoop
			}
			disk, err := readDiskInfo(diskPath)
			if err != nil {
				log.Println("Error while scan partition. Skip it: ", item.Path, diskPath, err)
				continue toScanLoop
			}
			for i, partition := range disk.Partitions {
				if partition.Number != partNumber {
					continue
				}
				item.Size = partition.Size()
				item.Partition = partition

				// Check if can extend partition.
				// Если можем расшириться за счет свободного места между разделами или до конца диска
				if i+1 < len(disk.Partitions) && disk.Partitions[i+1].IsFreeSpace() {
					freeSpace := disk.Partitions[i+1]
					item.FreeSpace = uint64(freeSpace.LastByte - partition.LastByte)
				}
			}
			storage = append(storage, item)

			// LVM_PV free space detection
			if item.Child != -1 && storage[item.Child].Type == type_LVM_PV {
				child := &storage[item.Child]
				newSize := lvmPVCalcSize(item.Size, child.LVMExtentSize)
				if newSize > child.Size {
					child.FreeSpace = newSize - child.Size
				}
			}
		case type_DISK:
			storage = append(storage, item)
			continue
		case type_LVM_LV:
			// Normalize path to LVM LV
			// Если был передан полный путь к LVM - заменяем его описанием из кеша, заполненного при сканировании LVM
			if major, minor := getMajorMinor(item.Path); major != 0 {
				item.Path = majorMinorDeviceTypeCache[[2]int{major, minor}].Path
			}
			item.Size = lvmLVGetSize(item.Path)
			storage = append(storage, item)

			lvm_group := storageItem{
				Type:  type_LVM_GROUP,
				Path:  item.Path[:strings.Index(item.Path, "/")],
				Child: len(storage) - 1,
			}
			toScan = append(toScan, lvm_group)
		case type_LVM_PV, type_LVM_PV_ADD:
			item.Size = lvmPVGetSize(item.Path)
			storage = append(storage, item)

			major, minor := getMajorMinor(item.Path)
			parent := storageItem{}
			parent.Path = item.Path
			parent.Child = len(storage) - 1
			parent.Type = getTypeByMajorMinor(major, minor)
			if parent.Type != type_UNKNOWN {
				toScan = append(toScan, parent)
			}
		case type_LVM_GROUP:
			item.Size, item.FreeSpace, item.LVMExtentSize = lvmVGGetSize(item.Path)
			storage = append(storage, item)
			lvmGroupIndex := len(storage) - 1

			// Find my and free pvs
			for _, pv := range getLvmPV() {
				if pv.VolumeGroup == "" {
					// Can use free LVM PV
					// Незанятые PV, можно использовать
					parent := storageItem{Path: pv.Path, Type: type_LVM_PV_ADD, Child: len(storage) - 1, LVMExtentSize: item.LVMExtentSize}

					// Calc usable PV size
					// для свободных pv  система выдает размер равный размеру раздела, так что испольузем расчетный размер
					parent.Size = lvmPVCalcSize(pv.Size, item.LVMExtentSize)
					toScan = append(toScan, parent)
				} else if pv.VolumeGroup == item.Path {
					// LVM PV in the LV group
					// PV, входящие в эту группу
					parent := storageItem{Path: pv.Path, Size: pv.Size, Type: type_LVM_PV, Child: len(storage) - 1, LVMExtentSize: item.LVMExtentSize}
					toScan = append(toScan, parent)
				} else {
					// nothing
				}
			}

			// Find free space for create new partition
			for _, part := range getNewPartitions() {
				pvCreate := storageItem{Child: lvmGroupIndex, Path: part.Path, Type: type_LVM_PV_NEW, LVMExtentSize: item.LVMExtentSize}
				storage = append(storage, pvCreate)
				partCreate := storageItem{Child: len(storage) - 1, Path: part.Path, Type: type_PARTITION_NEW, FreeSpace: part.Size(),
					Partition: part}
				storage = append(storage, partCreate)
			}
		}
	}

	// Fix free space for extend filesystem. We can't detect it while scan - on the step the program doesn't know partition/LVM size
	// Поправить свободной место файловой системы - оно не может быть определено просто во время, т.к. на этом шаге программа еще не знает размера нижележащего раздела/LVM
	for _, item := range storage {
		if item.Child != -1 && storage[item.Child].Type == type_FS {
			fs := &storage[item.Child]
			switch {
			case item.Size > fs.Size:
				fs.FreeSpace += item.Size - fs.Size
			case item.Size < fs.Size:
				log.Printf("WARNING: Filesystem size (%v) more then underliing layer (%v, %v)\n", fs.Path, item.Type, item.Path)
			case item.Size == fs.Size:
				// do nothing
			}
		}
	}

	return storage, err
}

func extractPartNumber(path string) (diskPath string, partNumber uint32, err error) {
	runePath := []rune(path)
	if !unicode.IsDigit(runePath[len(runePath)-1]) {
		return "", 0, fmt.Errorf("Can't extract part number from: %v", path)
	}
	startPartNumber := len(runePath) - 1
	for startPartNumber > 0 && unicode.IsDigit(runePath[startPartNumber-1]) {
		startPartNumber--
	}
	diskPathRunes := runePath[:startPartNumber]

	// If path have format /dev/loop0p1
	if len(diskPathRunes) > 1 && diskPathRunes[len(diskPathRunes)-1] == 'p' && unicode.IsDigit(diskPathRunes[len(diskPathRunes)-2]) {
		diskPathRunes = diskPathRunes[:len(diskPathRunes)-1]
	}
	diskPath = string(diskPathRunes)

	partNumber64, _ := parseUint(string(runePath[startPartNumber:]))
	return diskPath, uint32(partNumber64), nil
}

func fsGetSizeExt(path string) (size uint64, err error) {
	var blockCount, blockSize uint64
	var blockCountPresent, blockSizePresent bool
	res := cmdTrimLines("tune2fs", "-l", path)
	for _, line := range res {
		if strings.HasPrefix(line, "Block size:") {
			blockSizePresent = true
			blockSizeString := strings.TrimSpace(line[len("Block size:"):])
			blockSize, err = parseUint(blockSizeString)
			if err != nil {
				return
			}
		}
		if strings.HasPrefix(line, "Block count:") {
			blockCountString := strings.TrimSpace(line[len("Block count:"):])
			blockCountPresent = true
			blockCount, err = parseUint(blockCountString)
			if err != nil {
				return
			}
		}
	}
	if !blockCountPresent || !blockSizePresent {
		return 0, fmt.Errorf("Can't get filesistem size: %v", path)
	}
	size = blockCount * blockSize
	return
}

/*
path - пусть к блочному устройству, на котором расположена xfs
*/
func fsGetSizeXFS(path string) (size uint64, err error) {
	var tmpMountPath string
	if _, err = getMountPoint(path); err != nil {
		tmpMountPath, err = ioutil.TempDir("", "")
		if _, _, err = cmd("mount", "-t", "xfs", path, tmpMountPath); err != nil {
			return 0, fmt.Errorf("(fsGetSizeXFS) Can't xfs mount: %v", err)
		}
	}

	res := cmdTrimLines("xfs_info", path)
	if tmpMountPath != "" {
		cmd("umount", tmpMountPath)
		os.Remove(tmpMountPath)
	}

	for _, line := range res {
		if !strings.HasPrefix(line, "data ") {
			continue
		}
		// data     =                       bsize=4096   blocks=26240000, imaxpct=25
		// field 0  field 1                 field 2      field 3          field 4
		fields := strings.Fields(line)
		if !strings.HasPrefix(fields[2], "bsize=") || !strings.HasPrefix(fields[3], "blocks=") {
			continue
		}
		blockSize, err := parseUint(fields[2][len("bsize="):])
		if err != nil {
			return 0, err
		}
		blockCount, err := parseUint(fields[3][len("blocks=") : len(fields[3])-1]) // cut "blocks=" from start and "," from end.
		if err != nil {
			return 0, err
		}

		size = blockSize * blockCount
		return size, nil
	}
	return 0, fmt.Errorf("I can't find size of xfs filesystem: ", path)
}

// Return size of block device as it showed by kernel (in bytes)
func getDiskSize(path string) uint64 {
	for i := 0; i < TRY_COUNT; i++ {
		if i > 0 {
			log.Println("Try to read devsize once more: ", path)
			time.Sleep(time.Second)
		}
		res, errString, err := cmd("blockdev", "--getsize64", path)
		if err != nil {
			log.Println("Error while call blockdev: ", path, err, errString)
			continue
		}
		size, err := parseUint(strings.TrimSpace(res))
		if err == nil {
			return size
		} else {
			log.Println("Can't read block device size:", path, err)
		}
	}
	return 0
}

// return slice if all finded LVM PV
// Возвращает список всех известных lvmPV
func getLvmPV() []lvmPV {
	buf := &bytes.Buffer{}
	cmd := exec.Command("pvs", "-o", "pv_name,vg_name,pv_size", "--units", "B", "--separator", "|", "--noheading")
	cmd.Stdout = buf
	cmd.Run()

	res := make([]lvmPV, 0)
	for _, line := range strings.Split(buf.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lineParts := strings.Split(line, "|")
		sizeString := lineParts[2][:len(lineParts[2])-1]
		size, err := parseUint(sizeString)
		if err != nil {
			log.Println("Can't parse size: ", line, err)
			continue
		}
		res = append(res, lvmPV{Path: lineParts[0], VolumeGroup: lineParts[1], Size: size})
	}
	return res
}

func getMajorMinor(path string) (major, minor int) {
	for {
		linkDest, err := os.Readlink(path)
		if err == nil {
			if filepath.IsAbs(linkDest) {
				path = linkDest
			} else {
				path = filepath.Join(filepath.Dir(path), linkDest)
			}
		} else {
			break
		}
	}

	buf := &bytes.Buffer{}
	cmd := exec.Command("stat", "-c", "%t:%T", path)
	cmd.Stdout = buf
	cmd.Run()
	bufParts := strings.Split(buf.String(), ":")
	if len(bufParts) != 2 {
		return 0, 0
	}
	majorInt64, _ := strconv.ParseInt(strings.TrimSpace(bufParts[0]), 16, 32)
	minorInt64, _ := strconv.ParseInt(strings.TrimSpace(bufParts[1]), 16, 32)
	return int(majorInt64), int(minorInt64)
}

func getMountPoint(devPath string) (res string, err error) {
	originalMajor, originalMinor := getMajorMinor(devPath)
	if originalMajor == 0 {
		return "", fmt.Errorf("Can't get original major/minor numbers: ", devPath)
	}

	mountsBytes, err := ioutil.ReadFile("/proc/mounts")
	if err != nil {
		return
	}
	// Find mount point of the partition
	// Ищем точку монтирования указанного устройства
	mounts := string(mountsBytes)
	for _, line := range strings.Split(mounts, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		major, minor := getMajorMinor(fields[0])
		if major == originalMajor && minor == originalMinor {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("Can't find mountpoint of: ", devPath)
}

// Find and return partitions for create.
// Находит и возвращает описания разделов, которые можно создать на свободном дисковом пространстве.
func getNewPartitions() (res []partition) {
	disks := make(map[string]diskInfo)
	majorMinorCache := make(map[[2]int]string)

	// Scan disks
	// Сканируем диски
	filepath.Walk("/dev", func(path string, info os.FileInfo, err error) error {
		// If it isn't block device - skip
		// Если это не файл блочного устройства - пропускаем
		if !(info.Mode()&os.ModeDevice == os.ModeDevice && info.Mode()&os.ModeCharDevice == 0) {
			return nil
		}
		major, minor := getMajorMinor(path)
		if getTypeByMajorMinor(major, minor) != type_DISK {
			return nil
		}

		disk, err := readDiskInfo(path)
		if err != nil {
			return err
		}
		if _, ok := majorMinorCache[[2]int{disk.Major, disk.Minor}]; ok {
			//log.Printf("Skip '%v' by major-minor cache (%v,%v). Prev path: %v\n", path, disk.Major, disk.Minor, cachedPath)
			return nil
		}

		disks[disk.Path] = disk
		majorMinorCache[[2]int{disk.Major, disk.Minor}] = path
		return nil
	})

	// For every disk find places for create partition
	// Для каждого диска смотрим какие новые разделы можно создать.
diskLoop:
	for _, disk := range disks {
		if disk.PartTable != "msdos" && disk.PartTable != "gpt" {
			log.Printf("I can't work with non msdos/gpt table partition. TODO.: %v, %v", disk.Path, disk.PartTable)
			continue diskLoop
		}

		for _, part := range disk.Partitions {
			if !part.IsFreeSpace() {
				continue
			}
			if part.Size() >= min_SIZE_NEW_PARTITION {
				// Need store point to copy of current item state.
				// В for _, disk := range ... меняется сам экземпляр disk, а нам нужно сохранить ссылку на копию
				// текущего диска
				disk_copy := disk

				partNum := diskNewPartitionNum(*part.Disk)
				if partNum == 0 {
					log.Println("Can't create partition on disk. have no free partition table entries")
					continue
				}
				newPartition := partition{Disk: &disk_copy, FirstByte: part.FirstByte, LastByte: part.LastByte,
					Number: partNum,
				}
				newPartition.Path = newPartition.makePath()
				res = append(res, newPartition)
			}
		}
	}

	return res
}

// from https://git.kernel.org/cgit/linux/kernel/git/stable/linux-stable.git/tree/Documentation/devices.txt?id=v4.2
// and cat /proc/devices
// and call pvs,lvs for detection lvm device
func getTypeByMajorMinor(major, minor int) storageItemType {
	if res, ok := majorMinorDeviceTypeCache[[2]int{major, minor}]; ok {
		return res.Type
	}

	switch major {
	case 7:
		return type_DISK
	case 3, 22, 33, 34, 56, 57, 88, 89, 90, 91:
		if minor%64 == 0 {
			return type_DISK
		} else {
			return type_PARTITION
		}
	case 8, 65, 66, 67, 68, 69, 70, 71, 128, 129, 130, 131, 132, 133, 134, 135:
		if minor%16 == 0 {
			return type_DISK
		} else {
			return type_PARTITION
		}
	case 259:
		return type_PARTITION
	}
	return type_UNKNOWN
}

// Path - VolumeGroup/VolumeName
func lvmLVGetSize(path string) uint64 {
	buf := &bytes.Buffer{}
	cmd := exec.Command("lvs", "-o", "vg_name,lv_name,lv_size", "--units", "B", "--separator", "/", "--noheading")
	cmd.Stdout = buf
	cmd.Run()
	needPrefix := path + "/"
	for _, line := range strings.Split(buf.String(), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, needPrefix) {
			sizeString := line[len(needPrefix) : len(line)-1]
			lvmSize, err := strconv.ParseUint(sizeString, 10, 64)
			if err != nil {
				log.Println("Can't parse lvm size: ", path, sizeString, err)
				return 0
			}
			return lvmSize
		}
	}
	log.Println("Can't find lvm: " + path)
	return 0
}

func lvmPVCalcSize(partitionSize, extentSize uint64) (pvSize uint64) {
	extentCount := partitionSize / extentSize
	if lvm_PV_METADATA_RESERVED > extentCount {
		return 0
	}
	extentCount -= lvm_PV_METADATA_RESERVED
	return extentCount * extentSize
}

// From time to time pvs no return size of the pvs and return it in next call or after few seconds.
func lvmPVGetSize(path string) uint64 {
	res := lvmPVGetSizeTry(path)
	if res == 0 {
		log.Println("Error while get pvsize, try again: ", path)
		time.Sleep(5 * time.Second)
		res = lvmPVGetSizeTry(path)
	}
	return res
}

func lvmPVGetSizeTry(path string) uint64 {
	buf := &bytes.Buffer{}
	cmd := exec.Command("pvs", "-o", "pv_size", "--units", "B", "--separator", "|", "--noheading", path)
	cmd.Stdout = buf
	cmd.Run()
	line := trimSuffixB(strings.TrimSpace(buf.String()))
	pvSize, err := strconv.ParseUint(line, 10, 64)
	if err != nil {
		log.Println("Can't parse pv size: ", path, line, err)
		return 0
	}
	return pvSize
	log.Println("Can't find pv size: ", path, ":\n", buf.String())
	return 0
}

func lvmVGGetSize(vgName string) (size, freeSize, extentSize uint64) {
	res := cmdTrimLines("vgs", "--units", "B", "--separator", "/", "--noheading", "-o", "vg_name,vg_size,vg_free,vg_extent_size")
	for _, line := range res {
		lineParts := strings.Split(line, "/")
		if lineParts[0] == vgName {
			size, _ = parseUint(trimSuffixB(lineParts[1]))
			freeSize, _ = parseUint(trimSuffixB(lineParts[2]))
			extentSize, _ = parseUint(trimSuffixB(lineParts[3]))
			return
		}
	}
	log.Printf("Can't get VG size, can't find volume group: '%v'\n", vgName)
	return 0, 0, 0
}

func parseUint(s string) (res uint64, err error) {
	return strconv.ParseUint(s, 10, 64)
}

func readDiskInfo(path string) (disk diskInfo, err error) {
	disk.Path = path
	disk.Major, disk.Minor = getMajorMinor(path)

	blockSizeString, _, _ := cmd("blockdev", "--getss", disk.Path)
	disk.SectorSizeLogical, err = strconv.ParseUint(strings.TrimSpace(blockSizeString), 10, 64)
	if err != nil {
		log.Println("Can't get block size:", disk.Path, err)
		return
	}

	disk.Size = getDiskSize(disk.Path)
	if disk.Size == 0 {
		log.Println("Can't get disk size:", disk.Path)
		return
	}

	diskFile, err := os.Open(disk.Path)
	defer diskFile.Close()

	if err != nil {
		log.Println("Error open disk: ", disk.Path, err)
		return
	}

	var firstUsableDiskByte uint64
	var lastUsableDiskByte uint64

	// Try read mbr
	mbrTable, err := mbr.Read(diskFile)
	if err != nil {
		log.Println("Can't read mbr table")
		return
	}
	if mbrTable.IsGPT() {
		disk.PartTable = "gpt"
		var gptTable gpt.Table
		gptTable, err = gpt.ReadTable(diskFile, disk.SectorSizeLogical)
		if err != nil {
			log.Println("Can't read gpt table: ", disk.Path, err)
			return
		}
		firstUsableDiskByte = gptTable.Header.FirstUsableLBA * disk.SectorSizeLogical
		lastUsableDiskByte = gptTable.Header.LastUsableLBA*disk.SectorSizeLogical + disk.SectorSizeLogical - 1
		for i, gptPart := range gptTable.Partitions {
			if gptPart.IsEmpty() {
				continue
			}
			part := partition{
				Disk:      &disk,
				Number:    uint32(i + 1),
				FirstByte: gptPart.FirstLBA * disk.SectorSizeLogical,
				LastByte:  gptPart.LastLBA*disk.SectorSizeLogical + disk.SectorSizeLogical - 1,
			}
			part.Path = part.makePath()
			disk.Partitions = append(disk.Partitions, part)
		}
	} else {
		disk.PartTable = "msdos"
		firstUsableDiskByte = 512 * 63 // As parted - align for can convert to GPT in feauture.
		lastUsableDiskByte = disk.Size - 1
		for i, mbrPart := range mbrTable.GetAllPartitions() {
			if mbrPart.IsEmpty() {
				continue
			}
			part := partition{
				Disk:      &disk,
				Number:    uint32(i + 1),
				FirstByte: uint64(mbrPart.GetLBAStart()) * disk.SectorSizeLogical,
				LastByte:  (uint64(mbrPart.GetLBAStart())+uint64(mbrPart.GetLBALen()))*disk.SectorSizeLogical - 1,
			}
			part.Path = part.makePath()
			disk.Partitions = append(disk.Partitions, part)
		}
	}

	// number of partition doesn't depend from order on disk. Sort it by disk order.
	sort.Sort(partitionSortByFirstByte(disk.Partitions))

	// Fix first usable byte (need for non aligned mgr partitions)
	if len(disk.Partitions) > 0 && firstUsableDiskByte < disk.Partitions[0].FirstByte && disk.Partitions[0].FirstByte < min_SIZE_NEW_PARTITION {
		firstUsableDiskByte = disk.Partitions[0].FirstByte
	}

	// make free space pseudo partitions
	newPartitions := make([]partition, 0)
	var lastByte = firstUsableDiskByte - 1
	for i, part := range disk.Partitions {
		switch {
		case lastByte == part.FirstByte-1:
			newPartitions = append(newPartitions, part)
		case lastByte < part.FirstByte-1:
			newPart := partition{
				Disk:      &disk,
				Number:    0,
				FirstByte: lastByte + 1,
				LastByte:  part.FirstByte - 1,
			}
			newPartitions = append(newPartitions, newPart, part)
		default:
			log.Printf("ERROR!!!! Have overlap partitions!!!!\n%v - %v\n%#v", disk.Path, i, disk.Partitions)
			err = fmt.Errorf("OVERLAP PARTITIONS")
			return
		}
		lastByte = part.LastByte
	}

	if lastByte < lastUsableDiskByte {
		newPart := partition{
			Disk:      &disk,
			Number:    0,
			FirstByte: lastByte + 1,
			LastByte:  lastUsableDiskByte,
		}
		newPartitions = append(newPartitions, newPart)
	}
	disk.Partitions = newPartitions
	return
}

// Scan LVM logical volumes and store major,minor numbers of them for strong detection of LVM/non LVM block device.
// Сканирует LVM_LV, запоминает их major,minor номера устройств. Для надёжного определения что блочное устройство это
// LVM/не LVM.
func scanLVM() {
	buf := &bytes.Buffer{}
	cmd := exec.Command("lvs", "-a", "-o", "vg_name,lv_name,lv_kernel_major,lv_kernel_minor,lv_size", "--units", "B", "--separator", "/", "--noheading")
	cmd.Stdout = buf
	cmd.Run()
	for _, line := range strings.Split(buf.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lineParts := strings.Split(line, "/")
		path := lineParts[0] + "/" + lineParts[1]
		major, err := strconv.Atoi(lineParts[2])
		if err != nil {
			log.Println("scanLVM, Can't parse lvs (major): ", line, err)
			continue
		}
		minor, err := strconv.Atoi(lineParts[3])
		if err != nil {
			log.Println("scanLVM, Can't parse lvs (minor): ", line, err)
			continue
		}
		sizeString := lineParts[4][:len(lineParts[4])-1]
		size, err := strconv.ParseUint(sizeString, 10, 64)
		if err != nil {
			log.Println("scanLVM, Can't parse lvs (size): ", line, err)
			continue
		}
		majorMinorDeviceTypeCache[[2]int{major, minor}] = storageItem{Path: path, Type: type_LVM_LV, Size: size}
	}
}

// Trim B in end of line
func trimSuffixB(s string) string {
	l := len(s)
	if l > 0 && s[l-1] == 'B' {
		return s[:l-1]
	}
	return s
}

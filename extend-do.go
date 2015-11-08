package main

import (
	"fmt"
	"github.com/rekby/gpt"
	"github.com/rekby/mbr"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"time"
)

const MAX_UINT32 = 4294967295
const TRY_COUNT = 5 // Retry operations if it can and first is fail. For example - fast change LVM not always succesfully
// and need retry after few seconds.

func extendPrint(plan []storageItem) {
	addSize := make([]uint64, len(plan))
	for i, item := range plan {
		item.FreeSpace += addSize[i]
		if item.Child != -1 {
			addSize[item.Child] += item.FreeSpace
		}
		if item.Type == type_PARTITION && item.FreeSpace > 0 {
			fmt.Println(strconv.Itoa(i)+": ", item, "May need reboot")
		} else {
			fmt.Println(strconv.Itoa(i)+": ", item)
		}
	}
}

func extendDo(plan []storageItem) (needReboot bool) {
	for i := range plan {
		log.Println("DO ", strconv.Itoa(i)+":", plan[i])
		item := &plan[i]
		switch item.Type {
		case type_PARTITION:
			oldKernelSize := getKernelSize(item.Path)
			oldFreeSpace := item.FreeSpace
			switch item.Partition.Disk.PartTable {
			case "msdos":
				if item.Partition.Number > 4 {
					log.Println("WARNING: Can't work with partition number > 4 in msdos partition table.")
					continue
				}
				diskIO, err := os.OpenFile(item.Partition.Disk.Path, os.O_RDONLY|os.O_SYNC, 0)
				if err != nil {
					log.Println("Can't open disk: ", item.Partition.Disk.Path, err)
					diskIO.Close()
					continue
				}
				partTable, err := mbr.Read(diskIO)
				diskIO.Close()
				if err != nil {
					log.Println("Can't read partition table: ", item.Partition.Disk.Path, err)
					continue
				}
				newSize := item.Size + item.FreeSpace
				sectorSize := newSize / item.Partition.Disk.SectorSizeLogical
				if sectorSize > MAX_UINT32 {
					// Тут возможно окргление размера в меньшую сторону, но пока для простоты - просто пропускаем.
					log.Printf("New partition size greater then can be in msdos table. SKIP IT.")
					continue
				}
				partTable.GetPartition(int(item.Partition.Number)).SetLBALen(uint32(sectorSize))

				diskIO, err = os.OpenFile(item.Partition.Disk.Path, os.O_WRONLY|os.O_SYNC, 0)
				if err != nil {
					log.Println("Can't open disk (2): ", item.Partition.Disk.Path, err)
					diskIO.Close()
					continue
				}
				err = partTable.Write(diskIO)
				diskIO.Close()
				if err != nil {
					log.Println("WARNING!!!!!! Can't write new partition table. Disk partition table can be damaged check it.")
					continue
				}
				if item.Child != -1 {
					plan[item.Child].FreeSpace += item.FreeSpace
					item.Size += item.FreeSpace
					item.FreeSpace = 0
				}
				fmt.Printf("Partition resized: %v to %v (+%v)\n", item.Path, formatSize(item.Size), formatSize(oldFreeSpace))
			case "gpt":
				diskIO, err := os.OpenFile(item.Partition.Disk.Path, os.O_RDWR|os.O_SYNC, 0)
				defer diskIO.Close() // Have to be closed manually. Defer close - for protect only.
				if err != nil {
					if err != nil {
						log.Println("Can't open disk: ", item.Partition.Disk.Path, err)
						diskIO.Close()
						continue
					}
				}
				_, err = diskIO.Seek(int64(item.Partition.Disk.SectorSizeLogical), 0)
				if err != nil {
					log.Println("Can't seek gpt disk: ", item.Path, err)
					diskIO.Close()
					continue
				}
				gptTable, err := gpt.ReadTable(diskIO, uint32(item.Partition.Disk.SectorSizeLogical))
				if err != nil {
					log.Println("Can't read gpt table: ", item.Path, err)
					diskIO.Close()
					continue
				}
				if uint32(len(gptTable.Partitions)) < item.Partition.Number {
					log.Println("gpt bad partition number")
					diskIO.Close()
					continue
				}
				gptTable.Partitions[item.Partition.Number-1].LastLBA += item.FreeSpace / item.Partition.Disk.SectorSizeLogical
				if gptTable.Partitions[item.Partition.Number-1].LastLBA > gptTable.Header.LastUsableLBA {
					diskSizeInSectors := item.Partition.Disk.Size / item.Partition.Disk.SectorSizeLogical
					gptTable = gptTable.CreateTableForNewDiskSize(diskSizeInSectors)

					if gptTable.Partitions[item.Partition.Number-1].LastLBA > gptTable.Header.LastUsableLBA {
						log.Println("ATTENTION!!! Error in calc of GPT partition size", item.Path)
						diskIO.Close()
						continue
					}
				}

				err = gptTable.Write(diskIO)
				if err != nil {
					log.Println("WARNING!!! Write GPT PRIMARY TABLE error. DATA MAY BE LOST.", item.Path, err)
					diskIO.Close()
					continue
				}
				err = gptTable.CreateOtherSideTable().Write(diskIO)
				if err != nil {
					log.Println("WARNING!!! Write GPT SECONDARY TABLE error. DATA MAY BE LOST.", item.Path, err)
					diskIO.Close()
					continue
				}
				if item.Child != -1 {
					plan[item.Child].FreeSpace += item.FreeSpace
					item.Size += item.FreeSpace
					item.FreeSpace = 0
				}
				fmt.Printf("Partition resized: %v to %v (+%v)\n", item.Path, formatSize(item.Size), formatSize(oldFreeSpace))
			default:
				log.Printf("I don't know partition table: %v(%v)", item.Partition.Disk.PartTable, item.Path)
				continue
			}
			cmd("partprobe", item.Partition.Disk.Path)
			newKernelSize := getKernelSize(item.Path)
			if oldKernelSize == newKernelSize && oldFreeSpace != 0 {
				log.Println("NEED REBOOT!")
				needReboot = true
			}
		case type_PARTITION_NEW:
			switch item.Partition.Disk.PartTable {
			case "msdos":
				if item.Partition.Number > 4 {
					log.Println("WARNING: Can't create partition with number > 4 in msdos partition table.")
					continue
				}
				diskIO, err := os.OpenFile(item.Partition.Disk.Path, os.O_RDWR, 0)
				if err != nil {
					log.Println("Can't create partition: ", item.Path, err)
					diskIO.Close()
					continue
				}
				partTable, err := mbr.Read(diskIO)
				if err != nil {
					log.Println("Can't read mbr partition table: ", item.Path, err)
					diskIO.Close()
					continue
				}
				partition := partTable.GetPartition(int(item.Partition.Number))
				if partition == nil {
					log.Println("Can't get mbr partition: ", item.Path)
					diskIO.Close()
					continue
				}
				if !partition.IsEmpty() {
					log.Println("Mbr partition isn't empty: ", item.Path)
					diskIO.Close()
					continue
				}
				partition.SetType(mbr.PART_LVM)
				lbaStart := item.Partition.FirstByte / item.Partition.Disk.SectorSizeLogical
				if lbaStart >= MAX_UINT32 {
					log.Println("Can't create msdos partition - sector number overflow", item.Path)
					diskIO.Close()
					continue
				}
				partition.SetLBAStart(uint32(lbaStart))
				bytesLen := item.Partition.LastByte - item.Partition.FirstByte + 1
				lbaLen := (bytesLen) / item.Partition.Disk.SectorSizeLogical
				if bytesLen%item.Partition.Disk.SectorSizeLogical != 0 {
					lbaLen += 1
				}
				if uint64(partition.GetLBAStart())+lbaLen > MAX_UINT32 {
					lbaLen = uint64(MAX_UINT32 - partition.GetLBAStart())
				}
				partition.SetLBALen(uint32(lbaLen))
				if partTable.Check() != nil {
					log.Println("Bad partition table after virtual create partition ", item.Path, partTable.Check())
					diskIO.Close()
					continue
				}
				_, err = diskIO.Seek(0, 0)
				if err != nil {
					log.Println("Mbr, can't seek diskIO", err)
					diskIO.Close()
					continue
				}

				err = partTable.Write(diskIO)
				if err != nil {
					log.Println("Mbr, can't write", err)
					diskIO.Close()
					continue
				}
				diskIO.Close()
				cmd("partprobe", item.Partition.Disk.Path)
				fmt.Printf("Partition created: %v (%v)\n", item.Path, formatSize(lbaLen*item.Partition.Disk.SectorSizeLogical))
			case "gpt":
				diskIO, err := os.OpenFile(item.Partition.Disk.Path, os.O_RDWR, 0)
				if err != nil {
					log.Println("Can't open disk for new partition in gpt:", item.Partition.Disk.Path, err)
					diskIO.Close()
					continue
				}
				_, err = diskIO.Seek(int64(item.Partition.Disk.SectorSizeLogical), 0)
				if err != nil {
					log.Println("Can't seek disk read gpt table for new partition: ", item.Partition.Disk.Path, err)
					diskIO.Close()
					continue
				}
				gptTable, err := gpt.ReadTable(diskIO, uint32(item.Partition.Disk.SectorSizeLogical))
				if err != nil {
					log.Println("Can't read gpt table, new partition: ", item.Partition.Disk.Path, err)
					diskIO.Close()
					continue
				}
				if int(item.Partition.Number) >= len(gptTable.Partitions) || item.Partition.Number < 1 {
					log.Println("Bad partition number for create partition in gpt: ", item.Partition.Disk.Path, err)
					diskIO.Close()
					continue
				}
				part := &gptTable.Partitions[item.Partition.Number-1]
				part.FirstLBA = item.Partition.FirstByte / item.Partition.Disk.SectorSizeLogical
				part.LastLBA = item.Partition.LastByte / item.Partition.Disk.SectorSizeLogical
				part.Type = gpt.GUID_LVM

				if gptTable.Partitions[item.Partition.Number-1].LastLBA > gptTable.Header.LastUsableLBA {
					diskSizeInSectors := item.Partition.Disk.Size / item.Partition.Disk.SectorSizeLogical
					gptTable = gptTable.CreateTableForNewDiskSize(diskSizeInSectors)

					if gptTable.Partitions[item.Partition.Number-1].LastLBA > gptTable.Header.LastUsableLBA {
						log.Println("ATTENTION!!! Error in calc of GPT partition size", item.Path)
						diskIO.Close()
						continue
					}
				}
				err = gptTable.Write(diskIO)
				if err != nil {
					log.Println("WARNING ERROR WHILE WRITE PRIMARY GPT PARTITION TABLE: ", item.Partition.Disk.Path, err)
					diskIO.Close()
					continue
				}
				err = gptTable.CreateOtherSideTable().Write(diskIO)
				if err != nil {
					log.Println("WARNING ERROR WHILE WRITE SECONDARY GPT PARTITION TABLE: ", item.Partition.Disk.Path, err)
					diskIO.Close()
					continue
				}
				cmd("partprobe", item.Partition.Disk.Path)
				fmt.Printf("New GPT partition created: %v (%v)\n", item.Path, formatSize((part.LastLBA-part.FirstLBA+1)*item.Partition.Disk.SectorSizeLogical))
			default:
				log.Println("Can't create partition in unknown partition table: ", item.Partition.Path, item.Partition.Disk.PartTable)
			}

		case type_LVM_GROUP:
			if item.Child != -1 {
				plan[item.Child].FreeSpace = item.FreeSpace
			}
			fmt.Printf("Free space on LVM_GROUP '%v' %v\n", item.Path, formatSize(item.FreeSpace))
		case type_LVM_LV:
		retryLoop2:
			for retry := 0; retry < TRY_COUNT; retry++ {
				if retry > 0 {
					log.Println("Try extend LVM LV once more:", item.Path)
					time.Sleep(time.Second)
				}
				cmd("lvresize", "-l", "+100%FREE", item.Path)
				newSize := lvmLVGetSize(item.Path)
				addSpace := newSize - item.Size
				if plan[item.Child].FreeSpace > 0 && (addSpace == 0 || newSize == 0) {
					continue retryLoop2
				}
				fmt.Printf("Resize LVM_LV %v to %v(+%v)\n", item.Path, formatSize(newSize), formatSize(addSpace))
				item.Size = newSize
				item.FreeSpace = 0
				if item.Child != -1 {
					plan[item.Child].FreeSpace += addSpace
				}
				break retryLoop2
			}
		case type_LVM_PV:
		retryLoop:
			for retry := 9; retry < TRY_COUNT; retry++ {
				if retry > 0 {
					log.Println("Try to resize LVM PV once more:", item.Path)
				}
				cmd("pvresize", item.Path)
				newSize := lvmPVGetSize(item.Path)
				addSpace := newSize - item.Size
				if plan[item.Child].FreeSpace > 0 && (addSpace == 0 || newSize == 0) {
					continue retryLoop
				}
				if item.Child != -1 {
					plan[item.Child].FreeSpace += addSpace
				}
				fmt.Printf("LVM PV Resized: %v to %v (+%v)\n", item.Path, formatSize(newSize), formatSize(addSpace))
				item.FreeSpace -= addSpace
				item.Size = newSize
				break retryLoop
			}
		case type_LVM_PV_NEW:
			vg := plan[item.Child].Path
			oldSize, _, _ := lvmVGGetSize(vg)
		retryLoop3:
			for retry := 0; retry < TRY_COUNT; retry++ {
				cmd("pvcreate", item.Path)
				cmd("vgextend", vg, item.Path)
				newSize, _, _ := lvmVGGetSize(vg) // Yes - create LVM PV, but check size of LVM VG. It is OK.
				addSpace := newSize - oldSize
				if plan[item.Child].FreeSpace > 0 && (addSpace == 0 || newSize == 0) {
					fmt.Println("Try extend VG once more: ", vg, item.Path)
					time.Sleep(time.Second)
					continue retryLoop3
				} else {
					fmt.Printf("Add PV %v (+%v)\n", item.Path, formatSize(newSize-oldSize))
					break retryLoop3
				}
			}

		case type_FS:
		retryLoop4:
			for retry := 0; retry < TRY_COUNT; retry++ {
				switch item.FSType {
				case "ext3", "ext4":
					res, stderr, _ := cmd("resize2fs", "-f", item.Path)
					newSize, err := fsGetSizeExt(item.Path)
					if err != nil {
						log.Printf("ATTENTION: Can't read new size after fs resize. Log of resize:\nstdout:%v\nstderr:%v\n", res, stderr)
						continue retryLoop4
					}
					addSpace := newSize - item.Size
					if addSpace == 0 {
						log.Printf("Filesystem doesn't extend. Log of resize:\nstdout: %v\nstderr: %v\n", res, stderr)
						continue retryLoop4
					}
					item.FreeSpace -= addSpace
					item.Size = newSize
					fmt.Printf("Resize filesystem: %v to %v (+%v)\n", item.Path, formatSize(item.Size), formatSize(addSpace))
					break retryLoop4
				case "xfs":
					var tmpMountPoint string
					var mountPoint string
					if mountPoint, _ = getMountPoint(item.Path); mountPoint == "" {
						tmpMountPoint, err := ioutil.TempDir("", "")
						if _, _, err = cmd("mount", "-t", "xfs", item.Path, tmpMountPoint); err != nil {
							log.Printf("Can't xfs mount: %v", err)
						}
						mountPoint = tmpMountPoint
					}

					res, stderr, _ := cmd("xfs_growfs", mountPoint)
					newSize, err := fsGetSizeXFS(item.Path)

					if tmpMountPoint != "" {
						cmd("umount", tmpMountPoint)
						os.Remove(tmpMountPoint)
					}

					if err != nil {
						log.Printf("ATTENTION: Can't read new size after fs resize. Log of resize:\nstdout:%v\nstderr:%v\n", res, stderr)
						continue retryLoop4
					}
					addSpace := newSize - item.Size
					item.FreeSpace -= addSpace
					item.Size = newSize
					if addSpace == 0 {
						log.Printf("Filesystem doesn't extend. Log of resize:\nstdout: %v\nstderr: %v\n", res, stderr)
						continue retryLoop4
					}
					fmt.Printf("Resize filesystem: %v to %v (+%v)\n", item.Path, formatSize(item.Size), formatSize(addSpace))
					break retryLoop4
				default:
					log.Println("I don't know the filesystem: ", item.Path, item.FSType)
				}
			}
		default:
			log.Println("I don't know way to resize type: ", item.Type)
		}
	}
	return needReboot
}

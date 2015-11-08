package main

import (
	"fmt"
	"strconv"
)

/*
storage - description of storages hierarhy and ways of extend them. storage[0] - top of hierarchy, target of extend.
storage can be modify while work the function. You have to store copy of them if you need previous state.

storage - описание иерархии и возможных путей расширения раздела. storage[0] - вершина, целевая точка расширения.
в процессе работы функции storage может портиться. Если важно его сохранение нужно сохранить у себя копию.
*/
func extendPlan(storage []storageItem) (plan []storageItem) {
	/*
		When it can create new partition or extend current partition - always select extend.
		Если есть возможность расширить существующий раздел и создать новый на этом же месте - выбираем расширение
		уже сущуствующего
	*/
	for i, item := range storage {
		// For every partition, what can be extended
		// Для каждого раздела, который возможно расширить
		if item.Type == type_PARTITION && item.FreeSpace > 0 {
			// Find create partitions plan, which overlap with item and cancel create them.
			// ищем предположительно создаваемые разделы, которые пересекаются с расширением и отменяем их создание
			// - на данный момент предпочитаем расширение текущего раздела созданию нового
			for newI, newItem := range storage {
				if newI == i {
					continue
				}
				// If it isn't create of partition or create partition on other disk
				// Если это не создание нового раздела или создание раздела на другом диске
				if newItem.Type != type_PARTITION_NEW || newItem.Partition.Disk.Path != item.Partition.Disk.Path {
					continue
				}
				// If partition doesn't ovelap with item
				// Если раздел не пересекается с item в т.ч. при расширении item
				if newItem.Partition.LastByte < item.Partition.FirstByte ||
					newItem.Partition.FirstByte > item.Partition.LastByte+item.FreeSpace {
					continue
				}

				// Cancel create LVM PV for cancelled partition
				// Отменяем создание LVM PV на этом томе
				if newItem.Child != -1 && storage[newItem.Child].Type == type_LVM_PV_NEW {
					storage[newItem.Child].Type = type_UNKNOWN
				}

				// Cancel create partition
				// Выключаем создание нового раздела из дальнейшей работы
				storage[newI].Type = type_UNKNOWN

				// Decrease created partnumbers after this
				// Уменьшаем номера далее создаваемых разделов на этом же диске
				prevNum := newItem.Partition.Number
				diskMajor, diskMinor := newItem.Partition.Disk.Major, newItem.Partition.Disk.Minor
				changedPartitionPathes := make(map[string]string)
				for fixPartNumbersI := range storage {
					fixPartNumbersItem := &storage[fixPartNumbersI]
					part := &fixPartNumbersItem.Partition
					if fixPartNumbersItem.Type != type_PARTITION_NEW ||
						part.Disk.Major != diskMajor || part.Disk.Minor != diskMinor ||
						part.Number <= prevNum {
						continue
					}

					oldPath := fixPartNumbersItem.Path
					currentPartNum := part.Number
					part.Number = prevNum
					part.Path = part.makePath()
					fixPartNumbersItem.Path = part.Path
					prevNum = currentPartNum
					changedPartitionPathes[oldPath] = fixPartNumbersItem.Path
				}

				// Fix pathes of underliing of changed partitions
				// Пройтись по плану и поправить пути к разделам у которых изменились пути
				for fixPartPathesI := range storage {
					if newPath, ok := changedPartitionPathes[storage[fixPartPathesI].Path]; ok {
						storage[fixPartPathesI].Path = newPath
					}
				}
			}
		}
	}

	// map storage index and plan index. planIndex = planMap[storageIndex]
	// соответствие индексов storage индексам plan. planIndex = planMap[storageIndex]
	planMap := make(map[int]int)
	planMap[-1] = -1
	for i := len(storage) - 1; i >= 0; i-- {
		item := storage[i]
		if item.Type == type_UNKNOWN {
			continue
		}
		planMap[i] = len(plan)
		plan = append(plan, item)
	}

	// Can be placed other optimizations here.
	// Тут в будущем возможны какие-то оптимизации, например чтобы сократить количество ребутов если их надо несколько.

	// Fix child indexes
	// правим индексы
	for i := range plan {
		item := &plan[i]
		item.Child = planMap[item.Child]
	}
	return plan
}

func formatUInt(num uint64) string {
	return strconv.FormatUint(num, 10)
}

// http://stackoverflow.com/questions/1094841/reusable-library-to-get-human-readable-version-of-file-size
func formatSize(num uint64) string {
	size := float64(num)
	units := [...]string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB", "ZiB"}
	for _, unit := range units {
		if size < 1024.0 {
			return fmt.Sprintf("%.1f%v", size, unit)
		}
		size /= 1024.0
	}
	return fmt.Sprintf("%.1fYiB", size)
}

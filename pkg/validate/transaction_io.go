package validate

import (
	"sort"
)

func repairOperationDelta(operations []RepairOperation) ([]string, []PathRename, []string) {
	var changed, deleted []string
	var renamed []PathRename
	renameDestinations := make(map[string]string)
	for _, operation := range operations {
		if operation.Kind == RepairOperationRename {
			renameDestinations[operation.Path] = operation.DestinationPath
		}
	}
	for _, operation := range operations {
		switch operation.Kind {
		case RepairOperationWrite:
			path := operation.Path
			if destination := renameDestinations[path]; destination != "" {
				path = destination
			}
			changed = append(changed, path)
		case RepairOperationRename:
			renamed = append(renamed, PathRename{From: operation.Path, To: operation.DestinationPath})
			if operation.Content != nil && SourceHash(operation.Content) != operation.SourceHash {
				changed = append(changed, operation.DestinationPath)
			}
		case RepairOperationDelete:
			deleted = append(deleted, operation.Path)
		}
	}
	sort.Slice(renamed, func(i, j int) bool {
		if renamed[i].From != renamed[j].From {
			return renamed[i].From < renamed[j].From
		}
		return renamed[i].To < renamed[j].To
	})
	return sortedUnique(changed), renamed, sortedUnique(deleted)
}

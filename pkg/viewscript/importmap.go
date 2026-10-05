package viewscript

import (
	_ "embed"
	"encoding/json"
	"sort"
)

// importmap.json is the kit's import map: the bare specifiers a view may import
// and the kit file each resolves to. The kit build writes it into boot.js
// (web/vite.kit.config.ts), so the browser and `rzm validate views` share it.
//
//go:embed importmap.json
var importMapJSON []byte

var kitModules = func() map[string]string {
	var modules map[string]string
	if err := json.Unmarshal(importMapJSON, &modules); err != nil {
		panic("viewscript: invalid importmap.json: " + err.Error())
	}
	return modules
}()

// IsKitImport reports whether a bare specifier is in the kit's import map.
func IsKitImport(specifier string) bool {
	_, ok := kitModules[specifier]
	return ok
}

// KitImports lists the import map's bare specifiers in order.
func KitImports() []string {
	names := make([]string, 0, len(kitModules))
	for name := range kitModules {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

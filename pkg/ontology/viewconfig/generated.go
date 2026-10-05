package viewconfig

import "sort"

func GeneratedTypeID(name string) string {
	return "generated.type." + name + ".table"
}

func GeneratedInterfaceID(name string) string {
	return "generated.interface." + name + ".table"
}

func GeneratedIDs(typeNames []string, interfaceNames []string) map[string]struct{} {
	out := map[string]struct{}{}
	sort.Strings(typeNames)
	for _, name := range typeNames {
		if name != "" {
			out[GeneratedTypeID(name)] = struct{}{}
		}
	}
	sort.Strings(interfaceNames)
	for _, name := range interfaceNames {
		if name != "" {
			out[GeneratedInterfaceID(name)] = struct{}{}
		}
	}
	return out
}

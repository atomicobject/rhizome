package store

import (
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"strings"
)

func symbolRefKey(ref codeanchor.SymbolRef) string {
	key := fmt.Sprintf("%s|%s|%s", strings.ToLower(string(ref.Lang)), ref.Pkg, ref.Name)
	if ref.Lang == codeanchor.LangPhp {
		return fmt.Sprintf("%s|%t", key, ref.Member)
	}
	return key
}

func splitFQN(fqn string) (pkg string, name string) {
	fqn = strings.TrimSpace(fqn)
	if fqn == "" {
		return "", ""
	}
	if dot := strings.LastIndex(fqn, "."); dot >= 0 {
		return fqn[:dot], fqn[dot+1:]
	}
	return "", fqn
}

package codeanchor

import "testing"

func TestNormalizeSymbolPHP(t *testing.T) {
	cases := []struct {
		name string
		ref  SymbolRef
		want string
	}{
		{
			name: "namespace-only function uses backslash",
			ref:  SymbolRef{Lang: LangPhp, Pkg: `Acme\Theme`, Name: "boot"},
			want: `Acme\Theme\boot`,
		},
		{
			name: "class method uses double-colon",
			ref:  SymbolRef{Lang: LangPhp, Pkg: `Acme\Theme\Foo`, Name: "bar", Member: true},
			want: `Acme\Theme\Foo::bar`,
		},
		{
			name: "class itself (namespace-rooted) uses backslash",
			ref:  SymbolRef{Lang: LangPhp, Pkg: `Acme\Theme`, Name: "Foo"},
			want: `Acme\Theme\Foo`,
		},
		{
			name: "static member uses double-colon",
			ref:  SymbolRef{Lang: LangPhp, Pkg: `Acme\Theme\Foo`, Name: "INSTANCE", Member: true},
			want: `Acme\Theme\Foo::INSTANCE`,
		},
		{
			name: "no pkg returns bare name",
			ref:  SymbolRef{Lang: LangPhp, Name: "boot"},
			want: "boot",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeSymbol(tc.ref)
			if got != tc.want {
				t.Errorf("normalizeSymbol(%+v) = %q, want %q", tc.ref, got, tc.want)
			}
		})
	}
}

func TestParseSymbolSpecPHP(t *testing.T) {
	cases := []struct {
		name       string
		spec       string
		wantPkg    string
		wantName   string
		wantMember bool
	}{
		{
			name:       "method without lang prefix",
			spec:       `Polyglot\Todo\SyncClient::pushUpdates`,
			wantPkg:    `Polyglot\Todo\SyncClient`,
			wantName:   "pushUpdates",
			wantMember: true,
		},
		{
			name:       "namespaced function without lang prefix",
			spec:       `Acme\Theme\boot`,
			wantPkg:    `Acme\Theme`,
			wantName:   "boot",
			wantMember: false,
		},
		{
			name:       "namespaced function with php: prefix",
			spec:       `php:Acme\Theme\boot`,
			wantPkg:    `Acme\Theme`,
			wantName:   "boot",
			wantMember: false,
		},
		{
			name:       "class method with php: prefix",
			spec:       `php:Acme\Theme\Foo::bar`,
			wantPkg:    `Acme\Theme\Foo`,
			wantName:   "bar",
			wantMember: true,
		},
		{
			name:       "deeply nested namespace function",
			spec:       `Vendor\Module\Submodule\func_name`,
			wantPkg:    `Vendor\Module\Submodule`,
			wantName:   "func_name",
			wantMember: false,
		},
		{
			name:       "bare name (no separator)",
			spec:       "boot",
			wantPkg:    "",
			wantName:   "boot",
			wantMember: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSymbolSpec(tc.spec, LangPhp)
			if err != nil {
				t.Fatalf("parseSymbolSpec(%q) error: %v", tc.spec, err)
			}
			if got.Pkg != tc.wantPkg || got.Name != tc.wantName || got.Member != tc.wantMember {
				t.Errorf("parseSymbolSpec(%q) = {Pkg: %q, Name: %q, Member: %v}, want {Pkg: %q, Name: %q, Member: %v}",
					tc.spec, got.Pkg, got.Name, got.Member, tc.wantPkg, tc.wantName, tc.wantMember)
			}
		})
	}
}

func TestParseSymbolSpecPHPRoundTrip(t *testing.T) {
	specs := []string{
		`Acme\Theme\boot`,
		`Acme\Theme\Foo::bar`,
		`Vendor\Module\Submodule\func_name`,
		`Acme\Theme\Foo::CONST_NAME`,
		`Polyglot\Todo\SyncClient::pushUpdates`,
	}
	for _, spec := range specs {
		t.Run(spec, func(t *testing.T) {
			ref, err := parseSymbolSpec(spec, LangPhp)
			if err != nil {
				t.Fatalf("parseSymbolSpec error: %v", err)
			}
			got := normalizeSymbol(ref)
			if got != spec {
				t.Errorf("round trip: parseSymbolSpec(%q) → normalizeSymbol = %q, want %q", spec, got, spec)
			}
		})
	}
}

package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/appbundle"
	"github.com/stretchr/testify/require"
)

func cliLinkTestBundle(t *testing.T, parent, name, identifier string) string {
	t.Helper()
	root := filepath.Join(parent, name)
	target := filepath.Join(root, "Contents", "MacOS", "rzm")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte("fixture"), 0o755))
	plist := fmt.Sprintf(`<plist><dict><key>CFBundleIdentifier</key><string>%s</string><key>CFBundleShortVersionString</key><string>1.2.3</string></dict></plist>`, identifier)
	require.NoError(t, os.WriteFile(filepath.Join(root, "Contents", "Info.plist"), []byte(plist), 0o644))
	canonical, err := filepath.EvalSymlinks(target)
	require.NoError(t, err)
	return canonical
}

func cliLinkFixture(t *testing.T) (string, string) {
	t.Helper()
	oldMkdir := cliLinkMkdir
	oldDir, oldExe, oldSymlink, oldUnlink := cliLinkDirectory, desktopExecutable, cliLinkSymlink, cliLinkUnlink
	oldRun, oldPrivileged := cliLinkRunCommand, cliLinkPrivileged
	t.Cleanup(func() {
		cliLinkMkdir = oldMkdir
		cliLinkDirectory, desktopExecutable, cliLinkSymlink, cliLinkUnlink = oldDir, oldExe, oldSymlink, oldUnlink
		cliLinkRunCommand, cliLinkPrivileged = oldRun, oldPrivileged
	})
	parent, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	target := cliLinkTestBundle(t, parent, "Rhizome.app", "com.atomicobject.rhizome")
	cliLinkDirectory = filepath.Join(parent, "bin")
	require.NoError(t, os.MkdirAll(cliLinkDirectory, 0o755))
	desktopExecutable = func() (string, error) { return target, nil }
	cliLinkRunCommand = func(context.Context, string, ...string) (string, error) { return "", errors.New("no shell rzm") }
	cliLinkPrivileged = func(context.Context, string) error { t.Fatal("unexpected administrator prompt"); return nil }
	return target, filepath.Join(cliLinkDirectory, "rzm")
}

func TestCLILinkClassificationAndLifecycle(t *testing.T) {
	for _, state := range []string{"absent", "installed", "other-bundle", "dangling", "foreign-file", "foreign-link", "foreign-app", "dev", "unbundled"} {
		t.Run(state, func(t *testing.T) {
			target, link := cliLinkFixture(t)
			parent := filepath.Dir(cliLinkDirectory)
			expected := state
			var original string
			switch state {
			case "installed":
				original = target
			case "other-bundle":
				original = cliLinkTestBundle(t, parent, "Moved Rhizome.app", "com.atomicobject.rhizome")
			case "dangling":
				original = filepath.Join(parent, "Other.app", "Contents", "MacOS", "rzm")
			case "foreign-file":
				require.NoError(t, os.WriteFile(link, []byte("foreign"), 0o755))
				expected = "foreign"
			case "foreign-link":
				original = filepath.Join(parent, "missing-rzm")
				expected = "foreign"
			case "foreign-app":
				original = cliLinkTestBundle(t, parent, "Foreign.app", "example.foreign")
				expected = "foreign"
			case "dev":
				dev := cliLinkTestBundle(t, parent, "Rhizome Dev.app", "com.atomicobject.rhizome.dev")
				desktopExecutable = func() (string, error) { return dev, nil }
				expected = "unsupported"
			case "unbundled":
				desktopExecutable = func() (string, error) { return filepath.Join(parent, "rzm"), nil }
				expected = "unsupported"
			}
			if original != "" {
				require.NoError(t, os.Symlink(original, link))
			}
			service := &Service{}
			info, err := service.CLILinkStatus(context.Background())
			require.NoError(t, err)
			require.Equal(t, expected, info.State)
			require.Equal(t, link, info.LinkPath)
			require.Equal(t, "no shell rzm", info.Shell.Error)
			for _, remove := range []bool{false, true} {
				if remove {
					info, err = service.CLILinkRemove(context.Background())
				} else {
					info, err = service.CLILinkInstall(context.Background())
				}
				switch expected {
				case "foreign":
					code(t, err, "link_foreign")
				case "unsupported":
					want := "link_unsupported"
					if state == "dev" {
						want = "link_dev_bundle"
					}
					code(t, err, want)
				default:
					require.NoError(t, err)
					if remove {
						require.Equal(t, "absent", info.State)
					} else {
						require.Equal(t, "installed", info.State)
					}
				}
			}
			if expected == "foreign" {
				if original != "" {
					actual, err := os.Readlink(link)
					require.NoError(t, err)
					require.Equal(t, original, actual)
				} else {
					data, err := os.ReadFile(link)
					require.NoError(t, err)
					require.Equal(t, "foreign", string(data))
				}
			}
		})
	}
}

func TestCLILinkPrivilegedCommands(t *testing.T) {
	for _, state := range []string{"absent", "installed", "other-bundle", "dangling"} {
		t.Run(state, func(t *testing.T) {
			info := CLILinkInfo{LinkPath: "/usr/local/bin/rzm", State: state, Target: "/Applications/Old.app/Contents/MacOS/rzm", rawTarget: "/Applications/Old.app/Contents/MacOS/rzm"}
			unchanged := "[ -L '/usr/local/bin/rzm' ] && [ \"$(readlink '/usr/local/bin/rzm')\" = '/Applications/Old.app/Contents/MacOS/rzm' ]"
			if state == "absent" {
				unchanged = "[ ! -e '/usr/local/bin/rzm' ] && [ ! -L '/usr/local/bin/rzm' ]"
			}
			expected := "mkdir -p '/usr/local/bin' && " + unchanged + " && ln -sfn '/Applications/Rhizome.app/Contents/MacOS/rzm' '/usr/local/bin/rzm'"
			require.Equal(t, expected, cliLinkCommand(info, "/Applications/Rhizome.app/Contents/MacOS/rzm", false))
			require.Equal(t, []string{"-e", "on run argv", "-e", "do shell script item 1 of argv with administrator privileges", "-e", "end run", expected}, cliLinkAdminArgs(expected))
			require.Equal(t, "[ -L '/usr/local/bin/rzm' ] && [ \"$(readlink '/usr/local/bin/rzm')\" = '/Applications/Old.app/Contents/MacOS/rzm' ] && unlink '/usr/local/bin/rzm'", cliLinkCommand(info, "", true))
		})
	}
	require.Equal(t, `'a'"'"'b $(danger)'`, cliLinkQuote("a'b $(danger)"))
}

func TestCLILinkPermissionFallbackAndCancellation(t *testing.T) {
	for _, state := range []string{"absent", "other-bundle", "dangling"} {
		for _, outcome := range []string{"success", "cancelled", "failure"} {
			t.Run(state+"/"+outcome, func(t *testing.T) {
				target, link := cliLinkFixture(t)
				if state != "absent" {
					old := filepath.Join(filepath.Dir(cliLinkDirectory), "Moved.app", "Contents", "MacOS", "rzm")
					if state == "other-bundle" {
						old = cliLinkTestBundle(t, filepath.Dir(cliLinkDirectory), "Moved.app", "com.atomicobject.rhizome")
					}
					require.NoError(t, os.Symlink(old, link))
				}
				cliLinkSymlink = func(string, string) error { return &os.PathError{Op: "symlink", Path: link, Err: os.ErrPermission} }
				calls := 0
				cliLinkPrivileged = func(ctx context.Context, text string) error {
					calls++
					// The unprivileged attempt already removed an owned link, so the
					// administrator command expects nothing at the link path.
					want := "mkdir -p " + cliLinkQuote(cliLinkDirectory) + " && [ ! -e " + cliLinkQuote(link) + " ] && [ ! -L " + cliLinkQuote(link) + " ] && ln -sfn " + cliLinkQuote(target) + " " + cliLinkQuote(link)
					require.Equal(t, want, text)
					switch outcome {
					case "cancelled":
						return errors.New("execution error: User canceled. (-128)")
					case "failure":
						return errors.New("administrator command failed")
					default:
						return os.Symlink(target, link)
					}
				}
				info, err := (&Service{}).CLILinkInstall(context.Background())
				require.Equal(t, 1, calls)
				switch outcome {
				case "success":
					require.NoError(t, err)
					require.Equal(t, "installed", info.State)
				case "cancelled":
					code(t, err, "link_cancelled")
				case "failure":
					code(t, err, "link_failed")
				}
			})
		}
	}
}

func TestCLILinkRemovalPrivilegeAndRelativeTarget(t *testing.T) {
	target, link := cliLinkFixture(t)
	relative, err := filepath.Rel(filepath.Dir(link), target)
	require.NoError(t, err)
	require.NoError(t, os.Symlink(relative, link))
	cliLinkUnlink = func(string) error { return os.ErrPermission }
	calls := 0
	cliLinkPrivileged = func(ctx context.Context, text string) error {
		calls++
		require.Equal(t, "[ -L "+cliLinkQuote(link)+" ] && [ \"$(readlink "+cliLinkQuote(link)+")\" = "+cliLinkQuote(relative)+" ] && unlink "+cliLinkQuote(link), text)
		return os.Remove(link)
	}
	info, err := (&Service{}).CLILinkRemove(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, "absent", info.State)
}

func TestCLILinkDoesNotElevateOtherErrorsOrForeignReplacement(t *testing.T) {
	for _, scenario := range []string{"failure", "foreign-race"} {
		t.Run(scenario, func(t *testing.T) {
			_, link := cliLinkFixture(t)
			cliLinkSymlink = func(string, string) error {
				if scenario == "foreign-race" {
					require.NoError(t, os.WriteFile(link, []byte("foreign"), 0o755))
					return os.ErrPermission
				}
				return errors.New("unrelated filesystem error")
			}
			_, err := (&Service{}).CLILinkInstall(context.Background())
			want := "link_failed"
			if scenario == "foreign-race" {
				want = "link_foreign"
			}
			code(t, err, want)
		})
	}
}

func TestCLILinkLoginShellResolution(t *testing.T) {
	for _, scenario := range []string{"app", "other", "version-fails", "not-path", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			target, link := cliLinkFixture(t)
			require.NoError(t, os.Symlink(target, link))
			path := link
			if scenario == "other" {
				path = cliLinkTestBundle(t, filepath.Dir(cliLinkDirectory), "Other.app", "com.atomicobject.rhizome")
			}
			t.Setenv("SHELL", "/bin/fixture-shell")
			calls := 0
			cliLinkRunCommand = func(ctx context.Context, name string, args ...string) (string, error) {
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), 5*time.Second)
				calls++
				if calls == 1 {
					require.Equal(t, "/bin/fixture-shell", name)
					require.Equal(t, []string{"-lc", "command -v rzm"}, args)
					if scenario == "not-path" {
						return "rzm: function", nil
					}
					if scenario == "timeout" {
						return "", context.DeadlineExceeded
					}
					return path + "\n", nil
				}
				require.Equal(t, path, name)
				require.Equal(t, []string{"--version"}, args)
				if scenario == "version-fails" {
					return "", errors.New("version failed")
				}
				return "rzm version v1.2.3\n", nil
			}
			info, err := (&Service{}).CLILinkStatus(context.Background())
			require.NoError(t, err)
			switch scenario {
			case "app":
				require.Equal(t, path, info.Shell.Path)
				require.Equal(t, "v1.2.3", info.Shell.Version)
				require.True(t, info.Shell.IsApp)
			case "other":
				require.Equal(t, path, info.Shell.Path)
				require.False(t, info.Shell.IsApp)
			default:
				require.NotEmpty(t, info.Shell.Error)
			}
		})
	}
}

func TestCLILinkShellFallback(t *testing.T) {
	target, _ := cliLinkFixture(t)
	t.Setenv("SHELL", "")
	cliLinkRunCommand = func(_ context.Context, name string, args ...string) (string, error) {
		require.Equal(t, "/bin/zsh", name)
		return "", errors.New("no rzm")
	}
	bundle, err := appbundle.Resolve(target)
	require.NoError(t, err)
	require.Equal(t, "no rzm", cliLinkShell(context.Background(), bundle).Error)
}

func TestCLILinkRechecksBeforeUnlink(t *testing.T) {
	target, link := cliLinkFixture(t)
	other := cliLinkTestBundle(t, filepath.Dir(cliLinkDirectory), "Moved.app", "com.atomicobject.rhizome")
	require.NoError(t, os.Symlink(other, link))
	cliLinkMkdir = func(string, os.FileMode) error {
		require.NoError(t, os.Remove(link))
		return os.WriteFile(link, []byte("foreign"), 0o755)
	}
	_, err := (&Service{}).CLILinkInstall(context.Background())
	code(t, err, "link_foreign")
	data, err := os.ReadFile(link)
	require.NoError(t, err)
	require.Equal(t, "foreign", string(data))
	require.FileExists(t, target)
}

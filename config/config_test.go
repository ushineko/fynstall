package config

import (
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// write puts a config and a payload file in a temp dir and returns the
// config's path.
func write(t *testing.T, yaml string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "bin"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bin", "hello"), []byte("x"), 0o600))
	p := filepath.Join(dir, "fynstall.yaml")
	require.NoError(t, os.WriteFile(p, []byte(yaml), 0o600))
	return p
}

const valid = `app:
  id: io.example.hello
  name: Hello
  version: 0.1.0
payload:
  - src: bin/hello
    dst: bin/hello
`

func TestAValidConfigGetsDefaults(t *testing.T) {
	c, err := Load(write(t, valid))
	require.NoError(t, err)
	require.Equal(t, []string{"user"}, c.Install.Scopes)
	require.Equal(t, "{data}/{id}", c.Install.Dir["user"])
	require.Equal(t, "/opt/{id}", c.Install.Dir["system"])
}

func TestEveryErrorHasItsLineAndField(t *testing.T) {
	cases := []struct {
		name, yaml  string
		line        int
		field, want string
	}{
		{"unknown key", valid + "colour: blue\n", 8, "colour", "unknown key"},
		{"unknown nested key", "app:\n  id: io.example.hello\n  name: Hello\n  version: 0.1.0\n  icn: x.png\n" + valid[len("app:\n  id: io.example.hello\n  name: Hello\n  version: 0.1.0\n"):], 5, "icn", "unknown key"},
		{"missing version", "app:\n  id: io.example.hello\n  name: Hello\npayload:\n  - src: bin/hello\n    dst: bin/hello\n", 1, "app.version", "required"},
		{"bad id", "app:\n  id: hello\n  name: Hello\n  version: 0.1.0\npayload:\n  - src: bin/hello\n    dst: bin/hello\n", 2, "app.id", "reverse-DNS"},
		{"bad placeholder", valid + "install:\n  dir:\n    user: \"{datum}/{id}\"\n", 10, "install.dir.user", "unknown placeholder {datum}"},
		{"dst escapes", "app:\n  id: io.example.hello\n  name: Hello\n  version: 0.1.0\npayload:\n  - src: bin/hello\n    dst: ../hello\n", 7, "payload[0].dst", "leaves the install directory"},
		{"absolute dst", "app:\n  id: io.example.hello\n  name: Hello\n  version: 0.1.0\npayload:\n  - src: bin/hello\n    dst: /usr/bin/hello\n", 7, "payload[0].dst", "relative path"},
		{"missing src file", "app:\n  id: io.example.hello\n  name: Hello\n  version: 0.1.0\npayload:\n  - src: bin/nope\n    dst: bin/hello\n", 6, "payload[0].src", "does not exist"},
		{"no payload", "app:\n  id: io.example.hello\n  name: Hello\n  version: 0.1.0\n", 1, "payload", "at least one"},
		{"unknown target", valid + "targets: [plan9/386]\n", 8, "targets", "unknown target"},
		{"remove pattern leaves", valid + "uninstall:\n  remove: [\"python/**/__pycache__\", \"../cache\"]\n", 9, "uninstall.remove", "reaches outside the install directory"},
		{"remove pattern absolute", valid + "uninstall:\n  remove:\n    - /var/cache/x\n", 10, "uninstall.remove", "is absolute"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := write(t, tc.yaml)
			_, err := Load(p)
			var errs Errors
			require.True(t, errors.As(err, &errs), "%v", err)
			require.Len(t, errs, 1, "%v", errs)
			require.Equal(t, p, errs[0].File)
			require.Equal(t, tc.line, errs[0].Line, "%v", errs[0])
			require.Equal(t, tc.field, errs[0].Field)
			require.Contains(t, errs[0].Msg, tc.want)
		})
	}
}

func TestAllErrorsAreReportedAtOnceInLineOrder(t *testing.T) {
	_, err := Load(write(t, "app:\n  id: hello\n  name: Hello\n  version: one\npayload:\n  - src: bin/hello\n    dst: ../x\n"))
	var errs Errors
	require.True(t, errors.As(err, &errs))
	require.Len(t, errs, 3)
	require.Equal(t, []int{2, 4, 7}, []int{errs[0].Line, errs[1].Line, errs[2].Line})
}

func TestCheckDst(t *testing.T) {
	for dst, ok := range map[string]bool{
		"bin/hello": true, "share/": true, "a/../b": true,
		"..": false, "a/../../b": false, "/abs": false, `a\b`: false,
		".fynstall/receipt.json": false, "a:b": false, ".": false,
	} {
		require.Equal(t, ok, CheckDst(dst) == "", "%q: %s", dst, CheckDst(dst))
	}
}

func writePNG(t *testing.T, dir, name string, w, h int) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, name))
	require.NoError(t, err)
	require.NoError(t, png.Encode(f, image.NewNRGBA(image.Rect(0, 0, w, h))))
	require.NoError(t, f.Close())
}

func TestIntegrationKeysAreChecked(t *testing.T) {
	cases := []struct {
		name, extra string
		line        int
		field, want string
	}{
		{"non-square icon", "  icon: wide.png\n", 5, "app.icon", "must be square"},
		{"small icon", "  icon: small.png\n", 5, "app.icon", "at least 512"},
		{"icon not a png", "  icon: bin/hello\n", 5, "app.icon", "not a PNG"},
		{"duplicate link name", "integration:\n  path_links: [bin/hello, other/hello]\n", 9, "integration.path_links", `two links are named "hello"`},
		{"desktop without exec", "integration:\n  desktop:\n    - name: Hello\n", 10, "integration.desktop[0].exec", "required"},
		{"bad keyword", "integration:\n  desktop:\n    - name: Hello\n      exec: bin/hello\n      keywords: [\"a;b\"]\n", 12, "integration.desktop[0].keywords", "not a keyword"},
		{"bad category", "integration:\n  desktop:\n    - name: Hello\n      exec: bin/hello\n      categories: [\"Not one\"]\n", 12, "integration.desktop[0].categories", "not a category"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			yaml := "app:\n  id: io.example.hello\n  name: Hello\n  version: 0.1.0\n"
			if strings.HasPrefix(tc.extra, "  icon") {
				yaml += tc.extra + "payload:\n  - src: bin/hello\n    dst: bin/hello\n"
			} else {
				yaml += "payload:\n  - src: bin/hello\n    dst: bin/hello\n" + tc.extra
			}
			p := write(t, yaml)
			writePNG(t, filepath.Dir(p), "wide.png", 1024, 512)
			writePNG(t, filepath.Dir(p), "small.png", 256, 256)
			_, err := Load(p)
			var errs Errors
			require.True(t, errors.As(err, &errs), "%v", err)
			require.Len(t, errs, 1, "%v", errs)
			require.Equal(t, tc.line, errs[0].Line, "%v", errs[0])
			require.Equal(t, tc.field, errs[0].Field)
			require.Contains(t, errs[0].Msg, tc.want)
		})
	}
}

func TestADesktopEntryTakesTheAppIDByDefault(t *testing.T) {
	c, err := Load(write(t, valid+"integration:\n  desktop:\n    - name: Hello\n      exec: bin/hello\n"))
	require.NoError(t, err)
	require.Equal(t, "io.example.hello", c.Integration.Desktop[0].ID)
}

// multi writes a config with targets and per-target sources, and returns
// its path. Only the linux/amd64 build exists unless more are named.
func multi(t *testing.T, payload string, built ...string) string {
	t.Helper()
	p := write(t, "app:\n  id: io.example.hello\n  name: Hello\n  version: 0.1.0\npayload:\n"+payload+
		"targets: [linux/amd64, linux/arm64, windows/amd64]\n")
	for _, b := range append([]string{"linux-amd64/hello"}, built...) {
		f := filepath.Join(filepath.Dir(p), "build", filepath.FromSlash(b))
		require.NoError(t, os.MkdirAll(filepath.Dir(f), 0o750))
		require.NoError(t, os.WriteFile(f, []byte("x"), 0o600))
	}
	return p
}

func TestPerTargetSourcesAreCheckedPerTarget(t *testing.T) {
	entry := "  - src: build/{os}-{arch}/hello{exe}\n    dst: bin/hello{exe}\n"
	_, err := Load(multi(t, entry, "linux-arm64/hello", "windows-amd64/hello.exe"))
	require.NoError(t, err)

	_, err = Load(multi(t, entry, "windows-amd64/hello.exe"))
	var errs Errors
	require.True(t, errors.As(err, &errs), "%v", err)
	require.Len(t, errs, 1, "%v", errs)
	require.Equal(t, 6, errs[0].Line)
	require.Equal(t, "build/linux-arm64/hello does not exist (target linux/arm64)", errs[0].Msg)
}

func TestTargetPatterns(t *testing.T) {
	cases := []struct {
		name, entry, want string
		line              int
	}{
		{"a filter limits the check to its targets",
			"  - src: build/{os}-{arch}/hello\n    dst: bin/hello\n    targets: [linux/amd64]\n", "", 0},
		{"a pattern must be os/arch",
			"  - src: build/linux-amd64/hello\n    dst: bin/hello\n    targets: [linux]\n", `"linux" is not an os/arch pattern`, 8},
		{"a filter that matches no target",
			"  - src: build/linux-amd64/hello\n    dst: bin/hello\n    targets: [darwin/*]\n", "matches none of the targets", 8},
		{"an unknown build placeholder",
			"  - src: build/{platform}/hello\n    dst: bin/hello\n", "unknown placeholder {platform}", 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(multi(t, tc.entry))
			if tc.want == "" {
				require.NoError(t, err)
				return
			}
			var errs Errors
			require.True(t, errors.As(err, &errs), "%v", err)
			require.Len(t, errs, 1, "%v", errs)
			require.Equal(t, tc.line, errs[0].Line, "%v", errs[0])
			require.Contains(t, errs[0].Msg, tc.want)
		})
	}
}

func TestMatches(t *testing.T) {
	require.True(t, Matches(nil, "linux/amd64"))
	require.True(t, Matches([]string{"windows/*"}, "windows/amd64"))
	require.True(t, Matches([]string{"*/arm64"}, "linux/arm64"))
	require.False(t, Matches([]string{"windows/*", "*/arm64"}, "linux/amd64"))
}

func TestParametersAndConfigFilesAreChecked(t *testing.T) {
	head := "app:\n  id: io.example.hello\n  name: Hello\n  version: 0.1.0\npayload:\n  - src: bin/hello\n    dst: bin/hello\n"
	cases := []struct {
		name, extra, field, want string
		line                     int
	}{
		{"a valid pair", "parameters:\n  - name: server\n    default: x\nactions:\n  - config_file:\n      path: \"{config}/hello/config.yml\"\n      values: {server: \"{param:server}\"}\n", "", "", 0},
		{"a reserved name", "parameters:\n  - name: dir\n", "parameters[0].name", "installer's own flags", 9},
		{"a bad name", "parameters:\n  - name: Server\n", "parameters[0].name", "not a parameter name", 9},
		{"a secret with a default", "parameters:\n  - name: token\n    secret: true\n    default: abc\n", "parameters[0].default", "a secret has no default", 11},
		{"a path inside nothing", "actions:\n  - config_file:\n      path: /etc/hello.yml\n      values: {a: b}\n", "actions[0].config_file.path", "must start with {config}/", 10},
		{"no format", "actions:\n  - config_file:\n      path: \"{config}/hello/config\"\n      values: {a: b}\n", "actions[0].config_file.format", "say format", 9},
		{"an undeclared parameter", "actions:\n  - config_file:\n      path: \"{config}/hello.json\"\n      values: {a: \"{param:nope}\"}\n", "actions[0].config_file.values.a", "{param:nope} names no parameter", 11},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(write(t, head+tc.extra))
			if tc.want == "" {
				require.NoError(t, err)
				return
			}
			var errs Errors
			require.True(t, errors.As(err, &errs), "%v", err)
			require.Len(t, errs, 1, "%v", errs)
			require.Equal(t, tc.field, errs[0].Field)
			require.Equal(t, tc.line, errs[0].Line, "%v", errs[0])
			require.Contains(t, errs[0].Msg, tc.want)
		})
	}
}

func TestActionsAreChecked(t *testing.T) {
	head := "app:\n  id: io.example.hello\n  name: Hello\n  version: 0.1.0\npayload:\n  - src: bin/hello\n    dst: bin/hello\n" +
		"parameters:\n  - name: server\n    default: x\n  - name: token\n    secret: true\n" +
		"integration:\n  keep_on_uninstall: [\"{data}/{id}/data\"]\n"
	// head is 14 lines; the actions start on line 15.
	cases := []struct {
		name, extra, field, want string
		line                     int
	}{
		{"valid actions", "actions:\n  - service:\n      name: hello\n      exec: bin/hello\n      args: [serve]\n" +
			"  - run:\n      exec: bin/hello\n      args: [setup, \"{param:server}\"]\n      undo: [teardown]\n" +
			"  - run:\n      exec: bin/hello\n      undo: none\n" +
			"  - run:\n      on: uninstall\n      exec: bin/hello\n      args: [stop]\n      continue_on_error: true\n" +
			"  - migrate:\n      from: \"{data}/old-hello\"\n      to: \"{data}/{id}/data/old\"\n", "", "", 0},
		{"a bad service name", "actions:\n  - service:\n      name: hello.service\n      exec: bin/hello\n", "actions[0].service.name", "not a service name", 17},
		{"an exec outside", "actions:\n  - service:\n      name: hello\n      exec: ../hello\n", "actions[0].service.exec", "leaves the install directory", 18},
		{"a bad restart", "actions:\n  - service:\n      name: hello\n      exec: bin/hello\n      restart: sometimes\n", "actions[0].service.restart", "not one of", 19},
		{"no undo", "actions:\n  - run:\n      exec: bin/hello\n", "actions[0].run.undo", "required", 16},
		{"a bad undo", "actions:\n  - run:\n      exec: bin/hello\n      undo: never\n", "", "the word none", 0},
		{"a secret argument", "actions:\n  - run:\n      exec: bin/hello\n      args: [\"--token={param:token}\"]\n      undo: none\n", "actions[0].run.args", "is secret", 18},
		{"a secret in undo", "actions:\n  - run:\n      exec: bin/hello\n      undo: [\"{param:token}\"]\n", "actions[0].run.undo", "is secret", 18},
		{"a later hook point", "actions:\n  - run:\n      on: after_install\n      exec: bin/hello\n", "actions[0].run.on", "spec 003", 17},
		{"an undo on a hook", "actions:\n  - run:\n      on: uninstall\n      exec: bin/hello\n      undo: none\n", "actions[0].run.undo", "nothing to undo", 19},
		{"a migrate into nothing kept", "actions:\n  - migrate:\n      from: \"{data}/old\"\n      to: \"{data}/{id}/new\"\n", "actions[0].migrate.to", "keep_on_uninstall", 18},
		{"a migrate from a system path", "actions:\n  - migrate:\n      from: /var/lib/old\n      to: \"{data}/{id}/data\"\n", "actions[0].migrate.from", "must start with", 17},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(write(t, head+tc.extra))
			if tc.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.want)
			if tc.field == "" {
				return
			}
			var errs Errors
			require.True(t, errors.As(err, &errs), "%v", err)
			require.Len(t, errs, 1, "%v", errs)
			require.Equal(t, tc.field, errs[0].Field)
			require.Equal(t, tc.line, errs[0].Line, "%v", errs[0])
		})
	}
}

func TestASystemInstallHasNoHome(t *testing.T) {
	c := "app:\n  id: io.example.hello\n  name: Hello\n  version: 0.1.0\npayload:\n  - src: bin/hello\n    dst: bin/hello\n" +
		"install:\n  scopes: [user, system]\nintegration:\n  keep_on_uninstall: [\"{home}/.hello\"]\n"
	_, err := Load(write(t, c))
	var errs Errors
	require.True(t, errors.As(err, &errs), "%v", err)
	require.Len(t, errs, 1, "%v", errs)
	require.Equal(t, "integration.keep_on_uninstall", errs[0].Field)
	require.Equal(t, 11, errs[0].Line)
	require.Contains(t, errs[0].Msg, "does not have")

	_, err = Load(write(t, strings.Replace(c, "[user, system]", "[user]", 1)))
	require.NoError(t, err, "a per-user install has a home")
}

func TestADesktopEntryIsMatchedToItsWindowByItsID(t *testing.T) {
	c, err := Load(write(t, valid+"integration:\n  desktop:\n    - name: Hello\n      exec: bin/hello\n    - id: io.example.tool\n      name: Tool\n      exec: bin/hello\n      terminal: true\n"))
	require.NoError(t, err)
	require.Equal(t, "io.example.hello", c.Integration.Desktop[0].StartupWMClass, "Fyne's window class is the app ID")
	require.Empty(t, c.Integration.Desktop[1].StartupWMClass, "a terminal program has no window of its own")
}

package config

import (
	"errors"
	"os"
	"path/filepath"
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

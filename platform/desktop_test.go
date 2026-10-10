package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/manifest"
)

func TestExecArgumentsAreQuotedAsTheSpecificationSays(t *testing.T) {
	for in, want := range map[string]string{
		"/opt/hello/bin/hello": "/opt/hello/bin/hello",
		"/home/a b/hello":      `"/home/a b/hello"`,
		`/x/say "hi"`:          `"/x/say \"hi\""`,
		"/x/$HOME":             `"/x/\$HOME"`,
		"/x/`id`":              "\"/x/\\`id\\`\"",
		`/x/a\b`:               `"/x/a\\b"`,
		"/x/100%":              "/x/100%%",
	} {
		require.Equal(t, want, quoteExecArg(in), in)
	}
}

func TestABackslashInAQuotedArgumentIsWrittenAsFour(t *testing.T) {
	b := RenderDesktop(manifest.Desktop{Name: "Hello"}, `/home/a b\c/hello`, "", "")
	require.Contains(t, string(b), `Exec="/home/a b\\\\c/hello"`+"\n")
}

var yes = true //nolint:gochecknoglobals // a value to point at

// The keys a launcher and a taskbar read (a consumer's install.sh wrote
// them, spec 001 phase 6b) are written as given, and left out when unset.
func TestTheLauncherKeysAreWrittenAsGiven(t *testing.T) {
	full := string(RenderDesktop(manifest.Desktop{
		Name: "Clock", GenericName: "Wallpaper manager", Keywords: []string{"wallpaper", "kde"},
		StartupNotify: &yes, StartupWMClass: "io.example.clock",
	}, "/x/clock", "", ""))
	for _, want := range []string{"GenericName=Wallpaper manager\n", "Keywords=wallpaper;kde;\n", "StartupNotify=true\n", "StartupWMClass=io.example.clock\n"} {
		require.Contains(t, full, want)
	}
	bare := string(RenderDesktop(manifest.Desktop{Name: "Clock"}, "/x/clock", "", ""))
	for _, key := range []string{"GenericName", "Keywords", "StartupNotify", "StartupWMClass"} {
		require.NotContains(t, bare, key)
	}
}

func TestTheEntryPassesDesktopFileValidate(t *testing.T) {
	tool, err := exec.LookPath("desktop-file-validate")
	if err != nil {
		t.Skip("desktop-file-validate is not installed")
	}
	d := manifest.Desktop{
		ID: "io.example.hello", Name: "Hello", Comment: "Says hello", Args: []string{"--greet", "a b"},
		Categories: []string{"Utility"}, Icon: true,
		GenericName: "Greeter", Keywords: []string{"hello", "greeting card"}, StartupNotify: &yes, StartupWMClass: "io.example.hello",
	}
	p := filepath.Join(t.TempDir(), "io.example.hello.desktop")
	require.NoError(t, os.WriteFile(p, RenderDesktop(d, "/home/a b/.local/share/io.example.hello/bin/hello", "io.example.hello", "/home/a b/.local/share/io.example.hello/uninstall"), 0o600))
	out, err := exec.CommandContext(t.Context(), tool, p).CombinedOutput()
	require.NoError(t, err, string(out))
	require.Empty(t, strings.TrimSpace(string(out)), "no warnings either")
}

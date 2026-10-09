//go:build !nogui

package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"fyne.io/fyne/v2/widget"
	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynedesygn/fynetest"
	"github.com/ushineko/fynedesygn/wizard"

	"github.com/ushineko/fynstall/engine"
	"github.com/ushineko/fynstall/internal/snapshot"
	"github.com/ushineko/fynstall/manifest"
)

// fakeToken stands in for a secret parameter's value in these tests.
const fakeToken = "example-not-a-credential"

// guiFixture is a manifest with a payload file, a link, a launcher entry,
// a secret parameter and a config file, and a temp HOME to install into.
func guiFixture(t *testing.T) (*manifest.Manifest, Payload, Env, string) {
	t.Helper()
	home := t.TempDir()
	content := "#!/bin/sh\necho hello\n"
	sum := sha256.Sum256([]byte(content))
	m := &manifest.Manifest{
		Schema: manifest.Schema, Target: "linux/amd64", GUI: true,
		App:    manifest.App{ID: "io.example.hello", Name: "Hello", Version: "0.1.0"},
		Scopes: []string{"user"}, Dirs: map[string]string{"user": "{data}/{id}"},
		Files:   []manifest.File{{Path: "bin/hello", Size: int64(len(content)), SHA256: hex.EncodeToString(sum[:]), Mode: 0o755}},
		Links:   []manifest.Link{{Name: "hello", Target: "bin/hello"}},
		Desktop: []manifest.Desktop{{ID: "io.example.hello", Name: "Hello", Exec: "bin/hello"}},
		Parameters: []manifest.Parameter{
			{Name: "server", Label: "Server", Default: "https://example.invalid"},
			{Name: "token", Label: "Token", Secret: true, Required: true},
		},
		ConfigFiles: []manifest.ConfigFile{{Path: "{config}/hello/config.yml", Format: "yaml",
			Values: map[string]string{"server": "{param:server}", "token": "{param:token}"}}},
		Licence: "Some terms.",
		Launch:  "bin/hello",
	}
	p := Payload{Files: fstest.MapFS{"bin/hello": {Data: []byte(content)}}, Uninstaller: []byte("uninstaller")}
	e := Env{Getenv: func(k string) string { return map[string]string{"HOME": home}[k] }, ExeDir: t.TempDir()}
	return m, p, e, home
}

func TestTheWizardInstallsWithItsParameters(t *testing.T) {
	m, p, e, home := guiFixture(t)
	g, err := newInstallWizard(m, p, installFlags{scope: "user"}, e)
	require.NoError(t, err)
	w := wizard.Headless(fynetest.App(t), g.options)
	titles := []string{}
	for _, pg := range g.options.Pages {
		titles = append(titles, pg.Title())
	}
	require.Equal(t, []string{"Welcome", "Licence", "Settings", "Location", "Ready", "Installing", "Done"}, titles)

	w.Next() // Welcome
	fynetest.FindCheck(w.Content()).SetChecked(true)
	w.Next() // Licence
	require.Equal(t, "Settings", w.Current().Title())
	w.Next()
	require.Equal(t, "Settings", w.Current().Title(), "the required secret is empty")
	var masked *widget.Entry
	for _, en := range fynetest.All[*widget.Entry](w.Content()) {
		if en.Password {
			masked = en
		}
	}
	require.NotNil(t, masked, "a secret is a masked entry")
	// A made-up value; it never leaves the test's temporary home.
	masked.SetText(fakeToken)
	w.Next() // Settings
	require.Equal(t, "Location", w.Current().Title())
	w.Next() // Location
	require.Equal(t, "Ready", w.Current().Title())
	w.Next() // Install: the job runs inline headless
	require.Equal(t, "Installing", w.Current().Title())
	require.Equal(t, "Hello is installed.", w.Message())
	bars := fynetest.All[*widget.ProgressBar](w.Content())
	require.Len(t, bars, 1, "the progress page has the counting bar")
	require.Equal(t, 1.0, bars[0].Value)
	var size int64
	for _, f := range g.plan.Files {
		size += f.Size
	}
	n := len(g.plan.Files)
	require.NotNil(t, fynetest.FindLabel(w.Content(), countsText(engine.Counts{Files: n, FilesTotal: n, Bytes: size, BytesTotal: size})),
		"the status line ends at the plan's totals")

	root := filepath.Join(home, ".local", "share", "io.example.hello")
	b, err := os.ReadFile(filepath.Join(root, "bin", "hello"))
	require.NoError(t, err)
	require.Equal(t, "#!/bin/sh\necho hello\n", string(b))
	cfg, err := os.ReadFile(filepath.Join(home, ".config", "hello", "config.yml"))
	require.NoError(t, err)
	require.Equal(t, "server: https://example.invalid\ntoken: "+fakeToken+"\n", string(cfg))
	entry, err := os.ReadFile(filepath.Join(home, ".local", "share", "applications", "io.example.hello.desktop"))
	require.NoError(t, err)
	require.Contains(t, string(entry), "Exec="+filepath.Join(root, "uninstall")+" --gui\n", "a full build's entry has the Uninstall action")

	w.Next()
	require.Equal(t, "Done", w.Current().Title())
	w.Next()
	require.Equal(t, wizard.Finished, w.Result().Outcome)
	require.False(t, w.Result().Checks[LaunchNow])
}

func TestTheWizardHoldsInstallWhenThePlanCannotBeMade(t *testing.T) {
	m, p, e, home := guiFixture(t)
	m.Parameters, m.ConfigFiles, m.Licence = nil, nil, ""
	// A directory where the payload needs a file.
	require.NoError(t, os.MkdirAll(filepath.Join(home, "elsewhere", "bin", "hello"), 0o750))
	before, err := snapshot.Take(home)
	require.NoError(t, err)

	g, err := newInstallWizard(m, p, installFlags{scope: "user"}, e)
	require.NoError(t, err)
	w := wizard.Headless(fynetest.App(t), g.options)
	w.Next() // Welcome
	fynetest.FindEntry(w.Content()).SetText(filepath.Join(home, "elsewhere"))
	w.Next() // Location
	require.Equal(t, "Ready", w.Current().Title())
	require.Contains(t, w.Message(), "is a directory")
	w.Next()
	require.Equal(t, "Ready", w.Current().Title(), "Install is held")
	w.Back()
	w.Cancel()
	require.Equal(t, wizard.Cancelled, w.Result().Outcome)
	after, err := snapshot.Take(home)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestAnInstalledProgramGetsItsOwnUninstallerOffered(t *testing.T) {
	m, p, e, home := guiFixture(t)
	idx := filepath.Join(home, ".local", "share", "fynstall", "installs")
	require.NoError(t, os.MkdirAll(idx, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(idx, "io.example.hello.json"),
		[]byte(`{"root":"/x","uninstaller":"/x/uninstall","version":"0.0.9","scope":"user"}`), 0o600))
	g, err := newInstallWizard(m, p, installFlags{scope: "user"}, e)
	require.NoError(t, err)
	require.Len(t, g.options.Pages, 2)
	require.Equal(t, "Already installed", g.options.Pages[0].Title())
	require.Equal(t, "0.0.9", g.existing.Version)
}

func TestTheUninstallerAsksOnceThenRemoves(t *testing.T) {
	for _, skip := range []bool{false, true} {
		t.Run(fmt.Sprintf("skip=%v", skip), func(t *testing.T) {
			m, p, e, home := guiFixture(t)
			m.Parameters, m.ConfigFiles = nil, nil
			before, err := snapshot.Take(home)
			require.NoError(t, err)
			plan, err := engine.NewPlan(m, engine.Options{Env: e.Getenv, Uninstaller: p.Uninstaller})
			require.NoError(t, err)
			r, err := engine.Apply(context.Background(), plan, p.Files, p.Uninstaller, nil)
			require.NoError(t, err)

			o := uninstallConfirm(r, skip)
			require.Equal(t, "Uninstall Hello 0.1.0?", o.Question)
			c := wizard.HeadlessConfirm(fynetest.App(t), o)
			if !skip {
				_, err := os.Stat(r.Root)
				require.NoError(t, err, "nothing is removed before the question is answered")
				c.Act()
			}
			require.Equal(t, "Hello 0.1.0 was removed.", c.Message())
			after, err := snapshot.Take(home)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

// The wizard, driven headless against the real engine.

//go:build !nogui

package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"fyne.io/fyne/v2/widget"
	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynedesygn/fynetest"
	"github.com/ushineko/fynedesygn/wizard"

	"github.com/ushineko/fynstall/engine"
	"github.com/ushineko/fynstall/internal/regtest"
	"github.com/ushineko/fynstall/internal/snapshot"
	"github.com/ushineko/fynstall/manifest"
	"github.com/ushineko/fynstall/platform"
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
		Schema: manifest.Schema, Target: runtime.GOOS + "/" + runtime.GOARCH, GUI: true,
		App:    manifest.App{ID: "io.example.hello", Name: "Hello", Version: "0.1.0"},
		Scopes: []string{"user"}, Dirs: map[string]string{"user": "{programs}/{id}"},
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
	run := t.TempDir()
	vars := map[string]string{"HOME": home, "USERPROFILE": home, "XDG_RUNTIME_DIR": run, regtest.Env: regtest.Root(t)}
	e := Env{Getenv: func(k string) string { return vars[k] }, ExeDir: t.TempDir()}
	if runtime.GOOS == "windows" {
		// Before any install: a fixture whose registry writes would reach
		// the real registry stops here.
		v, err := platform.Vars("user", e.Getenv)
		require.NoError(t, err)
		require.True(t, vars[regtest.Env] != "" && strings.HasPrefix(platform.RegKey(v, `HKCU\x`), `HKCU\`+vars[regtest.Env]+`\`),
			"the test's registry root is not in use; refusing to run an install against the real registry")
	}
	return m, p, e, home
}

// at is a path under the placeholder key ("data", "config", "programs"),
// wherever this OS puts it in the fixture's home.
func at(t *testing.T, e Env, key string, elem ...string) string {
	t.Helper()
	v, err := platform.Vars("user", e.Getenv)
	require.NoError(t, err)
	return filepath.Join(append([]string{v[key]}, elem...)...)
}

// snap is the fixture's home and, on Windows, its registry: both are part
// of "as it was" (R9d).
func snap(t *testing.T, e Env, home string) map[string]string {
	t.Helper()
	s, err := snapshot.Take(home)
	require.NoError(t, err)
	for k, v := range regtest.Snapshot(t, e.Getenv(regtest.Env)) {
		s["registry:"+k] = v
	}
	return s
}

func TestTheWizardInstallsWithItsParameters(t *testing.T) {
	m, p, e, _ := guiFixture(t)
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

	root := at(t, e, "programs", "io.example.hello")
	b, err := os.ReadFile(filepath.Join(root, "bin", "hello"))
	require.NoError(t, err)
	require.Equal(t, "#!/bin/sh\necho hello\n", string(b))
	cfg, err := os.ReadFile(at(t, e, "config", "hello", "config.yml"))
	require.NoError(t, err)
	require.Equal(t, "server: https://example.invalid\ntoken: "+fakeToken+"\n", string(cfg))
	if platform.Integration == platform.WindowsShell {
		// Settings > Apps opens the uninstaller's window (L8).
		got, ok := regtest.Get(t, e.Getenv(regtest.Env), platform.UninstallKey+`\io.example.hello`, "UninstallString")
		require.True(t, ok)
		require.Equal(t, `"`+filepath.Join(root, engine.UninstallName)+`" --gui`, got)
		v, err := platform.Vars("user", e.Getenv)
		require.NoError(t, err)
		_, err = os.Stat(filepath.Join(platform.StartMenu(v), "Hello.lnk"))
		require.NoError(t, err, "the launcher entry is a Start Menu shortcut")
	} else {
		entry, err := os.ReadFile(at(t, e, "data", "applications", "io.example.hello.desktop"))
		require.NoError(t, err)
		require.Contains(t, string(entry), "Exec="+filepath.Join(root, "uninstall")+" --gui\n", "a full build's entry has the Uninstall action")
	}

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
	before := snap(t, e, home)

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
	require.Equal(t, before, snap(t, e, home))
}

func TestAnInstalledProgramGetsItsOwnUninstallerOffered(t *testing.T) {
	m, p, e, _ := guiFixture(t)
	idx := at(t, e, "data", "fynstall", "installs")
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
			before := snap(t, e, home)
			plan, err := engine.NewPlan(m, engine.Options{Env: e.Getenv, Uninstaller: p.Uninstaller})
			require.NoError(t, err)
			r, err := engine.Apply(context.Background(), plan, p.Files, p.Uninstaller, nil)
			require.NoError(t, err)

			o, done := uninstallConfirm(r, skip, e.Getenv)
			defer done()
			require.Equal(t, "Uninstall Hello 0.1.0?", o.Question)
			c := wizard.HeadlessConfirm(fynetest.App(t), o)
			if !skip {
				_, err := os.Stat(r.Root)
				require.NoError(t, err, "nothing is removed before the question is answered")
				c.Act()
			}
			require.Equal(t, "Hello 0.1.0 was removed.", c.Message())
			require.Equal(t, before, snap(t, e, home))
		})
	}
}

// The program made a file in the install directory: the window lists it
// after the uninstall, and removes it only when asked.
func TestTheUninstallWindowOffersToRemoveTheLeftovers(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(fmt.Sprintf("remove=%v", remove), func(t *testing.T) {
			m, p, e, home := guiFixture(t)
			m.Parameters, m.ConfigFiles = nil, nil
			before := snap(t, e, home)
			plan, err := engine.NewPlan(m, engine.Options{Env: e.Getenv, Uninstaller: p.Uninstaller})
			require.NoError(t, err)
			r, err := engine.Apply(context.Background(), plan, p.Files, p.Uninstaller, nil)
			require.NoError(t, err)
			made := filepath.Join(r.Root, "state.db")
			require.NoError(t, os.WriteFile(made, []byte("the program's"), 0o600))

			o, done := uninstallConfirm(r, false, e.Getenv)
			defer done()
			c := wizard.HeadlessConfirm(fynetest.App(t), o)
			c.Act()
			require.Equal(t, "Hello left 1 file it made.", c.Question())
			require.Equal(t, "Hello 0.1.0 was removed.", c.Message())
			if !remove {
				c.Cancel() // Keep them
				require.Equal(t, wizard.Finished, c.Result().Outcome)
				_, err := os.Stat(made)
				require.NoError(t, err, "kept")
				return
			}
			c.Act()
			require.Equal(t, "Hello 0.1.0 was removed, with the files it made.", c.Message())
			require.Equal(t, before, snap(t, e, home))
		})
	}
}

func TestLeftoversDetailListsAtMostFiftyByName(t *testing.T) {
	var left []engine.Leftover
	for i := range 60 {
		left = append(left, engine.Leftover{Path: fmt.Sprintf("/x/f%02d", i)})
	}
	d := leftoversDetail(left)
	require.Contains(t, d, "`/x/f49`")
	require.NotContains(t, d, "`/x/f50`")
	require.Contains(t, d, "- and 10 more")
}

// With both scopes on offer, the wizard asks who the install is for, and
// each choice has its own location page and default (spec 001 phase 5).
func TestTheWizardAsksWhoTheInstallIsFor(t *testing.T) {
	m, p, e, _ := guiFixture(t)
	m.Parameters, m.ConfigFiles, m.Licence = nil, nil, ""
	m.Scopes = []string{"user", "system"}
	m.Dirs["system"] = "/opt/{id}"
	g, err := newInstallWizard(m, p, installFlags{scope: "user"}, e)
	require.NoError(t, err)
	if _, err := platform.Vars("system", e.Getenv); errors.Is(err, platform.ErrScopeUnavailable) {
		// Until this platform has system scope, the wizard offers the one
		// it has and does not ask.
		for _, pg := range g.options.Pages {
			require.NotEqual(t, "Install for", pg.Title())
		}
		require.Contains(t, g.dirs, "user")
		require.NotContains(t, g.dirs, "system")
		return
	}
	w := wizard.Headless(fynetest.App(t), g.options)
	w.Next() // Welcome
	require.Equal(t, "Install for", w.Current().Title())
	radio := fynetest.All[*widget.RadioGroup](w.Content())
	require.Len(t, radio, 1)
	require.Equal(t, ForMe, radio[0].Selected, "the first scope is the default")

	w.Next()
	require.Equal(t, "Location", w.Current().Title())
	require.Equal(t, g.dirs["user"].DirectoryPage, w.Current().(*scopedDir).DirectoryPage)
	require.Contains(t, g.dirs["user"].Value(), ".local/share/io.example.hello")
	w.Back()
	radio[0].SetSelected(ForEveryone)
	w.Next()
	require.Equal(t, "/opt/io.example.hello", w.Current().(*scopedDir).Value(), "the system page, with its own default")
	w.Next()
	require.Equal(t, "Ready", w.Current().Title())
	require.Equal(t, "system", g.plan.Scope)
	require.Contains(t, g.welcome(), "asks for an administrator once")
}

// The uninstall window of a system install keeps its helper waiting while
// it asks about the leftovers, and believes a removal only when the helper
// confirms it. A desk check found the window closing the helper's input
// when the first job returned, and then reporting a removal that never
// happened.
func TestTheUninstallWindowKeepsItsHelperForTheAnswer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the helper on Windows comes with UAC (spec 001 phase 7c); this one is a shell script")
	}
	if os.Geteuid() == 0 {
		t.Skip("as root there is no helper")
	}
	dir := t.TempDir()
	pass := filepath.Join(dir, "elevate")
	require.NoError(t, os.WriteFile(pass, []byte("#!/bin/sh\nexec \"$@\"\n"), 0o700)) // #nosec G306 -- a test program
	helperFor := func(confirms bool) string {
		ack := ""
		if confirms {
			ack = `if [ "$answer" = remove-leftovers ]; then echo '{"kind":"removed","step":0}'; fi` + "\n"
		}
		p := filepath.Join(dir, fmt.Sprintf("helper-%v", confirms))
		script := "#!/bin/sh\necho '{\"kind\":\"leftovers\",\"step\":0,\"leftovers\":[{\"Path\":\"/opt/x/state.db\"}]}'\n" +
			"read answer\n" + ack + "echo '{\"kind\":\"done\",\"step\":0}'\n"
		require.NoError(t, os.WriteFile(p, []byte(script), 0o700)) // #nosec G306 -- a test program
		return p
	}
	orig := helperProgram
	t.Cleanup(func() { helperProgram = orig })
	r := &engine.Receipt{App: manifest.App{ID: "io.example.hello", Name: "Hello", Version: "0.1.0"}, Scope: "system", Root: "/opt/x"}
	env := func(k string) string { return map[string]string{"FYNSTALL_ELEVATE": pass}[k] }

	for _, c := range []struct {
		name     string
		confirms bool
		remove   bool
		want     string
	}{
		{"removed", true, true, "Hello 0.1.0 was removed, with the files it made."},
		{"kept", true, false, ""},
		{"a helper that does not confirm", false, true, "Failed: the files the program made were not removed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			helper := helperFor(c.confirms)
			helperProgram = func() (string, error) { return helper, nil }
			o, done := uninstallConfirm(r, false, env)
			w := wizard.HeadlessConfirm(fynetest.App(t), o)
			w.Act()
			require.Equal(t, "Hello left 1 file it made.", w.Question())
			// A person takes a moment to answer. A helper whose input were
			// tied to the finished job would have seen it close by now.
			time.Sleep(200 * time.Millisecond)
			if !c.remove {
				w.Cancel() // Keep them
				done()
				require.Equal(t, wizard.Finished, w.Result().Outcome)
				return
			}
			w.Act()
			done()
			require.Contains(t, w.Message(), c.want)
		})
	}
}

// With the app installed, the first page says what this installer does to
// it (R17), and a downgrade waits for its check.
func TestTheWizardSaysWhatItDoesToAnInstalledVersion(t *testing.T) {
	for _, c := range []struct {
		installed, title, says string
		confirm                bool
	}{
		{"0.0.9", "Upgrade", "Upgrades Hello 0.0.9 to 0.1.0", false},
		{"0.1.0", "Repair", "Repairs Hello 0.1.0", false},
		{"0.2.0", "Downgrade", "Replaces Hello 0.2.0 with the older 0.1.0", true},
	} {
		t.Run(c.title, func(t *testing.T) {
			m, p, e, _ := guiFixture(t)
			m.Parameters, m.ConfigFiles, m.Licence = nil, nil, ""
			old := *m
			old.App.Version = c.installed
			plan, err := engine.NewPlan(&old, engine.Options{Env: e.Getenv, Uninstaller: p.Uninstaller})
			require.NoError(t, err)
			_, err = engine.Apply(context.Background(), plan, p.Files, p.Uninstaller, nil)
			require.NoError(t, err)

			g, err := newInstallWizard(m, p, installFlags{scope: "user"}, e)
			require.NoError(t, err)
			titles := []string{}
			for _, pg := range g.options.Pages {
				titles = append(titles, pg.Title())
			}
			require.Equal(t, []string{c.title, "Ready", "Installing", "Done"}, titles, "no location: a new version goes where the old one is")
			w := wizard.Headless(fynetest.App(t), g.options)
			var text []string
			for _, l := range fynetest.All[*widget.Label](w.Content()) {
				text = append(text, l.Text)
			}
			require.Contains(t, strings.Join(text, "\n"), c.says)
			require.NotNil(t, fynetest.FindButton(w.Content(), UninstallInstead))
			checks := fynetest.All[*widget.Check](w.Content())
			if !c.confirm {
				require.Empty(t, checks)
				w.Next()
				require.Equal(t, "Ready", w.Current().Title())
				require.Equal(t, plan.Root, g.plan.Root)
				return
			}
			w.Next()
			require.Equal(t, "Downgrade", w.Current().Title(), "a downgrade waits for its check")
			require.Len(t, checks, 1)
			checks[0].SetChecked(true)
			w.Next()
			require.Equal(t, "Ready", w.Current().Title())
		})
	}
}

package installer

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ushineko/fynstall/engine"
	"github.com/ushineko/fynstall/manifest"
	"github.com/ushineko/fynstall/platform"
)

/*
An upgrade, a repair and a downgrade are the same thing (spec 001 R17): the
installed version's own uninstaller removes it, then this version installs
fresh, so whatever the old version put there is taken away by the code that
put it there. The parameters it was given are read first, so the person is
not asked again, and its kept paths survive by being kept.

The plan shown is the one for after the old version is gone. Once it is,
the plan is made again; the install goes ahead only when the two have the
same content.
*/

// installed is an install of the app that this installer would replace.
type installed struct {
	ix      *engine.Index
	receipt *engine.Receipt
}

// findExisting looks for an install of m in scope, then in its other
// scopes: an install for everyone and one for the person are both the
// same program to them. It returns nil when there is none.
func findExisting(m *manifest.Manifest, scope string, getenv func(string) string) (*engine.Index, error) {
	scopes := append([]string{scope}, m.Scopes...)
	for _, s := range slices.Compact(scopes) {
		if !slices.Contains(m.Scopes, s) {
			continue
		}
		ix, _, err := engine.ReadIndex(m, s, getenv)
		switch {
		case err == nil:
			if ix.Scope == "" {
				ix.Scope = s
			}
			return ix, nil
		case !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, platform.ErrScopeUnavailable):
			return nil, err
		}
	}
	return nil, nil
}

// readExisting reads the receipt of the install ix names.
func readExisting(ix *engine.Index) (*installed, error) {
	r, err := engine.ReadReceipt(engine.ReceiptPath(ix.Root))
	if err != nil {
		return nil, fmt.Errorf("%w; if its record is lost, remove it with --force-receipt-uninstall", err)
	}
	return &installed{ix: ix, receipt: r}, nil
}

// replacement says what installing version to over an install of from is.
func replacement(from, to string) string {
	switch manifest.CompareVersions(to, from) {
	case 1:
		return "upgrade"
	case -1:
		return "downgrade"
	}
	return "repair"
}

// replacementText is one line for the person: "Upgrades Hello 0.1.0 to
// 0.2.0".
func replacementText(m *manifest.Manifest, from string) string {
	switch replacement(from, m.App.Version) {
	case "upgrade":
		return fmt.Sprintf("Upgrades %s %s to %s", m.App.Name, from, m.App.Version)
	case "downgrade":
		return fmt.Sprintf("Replaces %s %s with the older %s", m.App.Name, from, m.App.Version)
	}
	return fmt.Sprintf("Repairs %s %s", m.App.Name, from)
}

// oldUninstallArgs are the arguments for the installed uninstaller at
// path: --upgrade when it knows the flag, else --quiet, which removes the
// same files without asking. Every uninstaller ever installed is run with
// one of the two (R9e).
func oldUninstallArgs(ctx context.Context, path string) []string {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, path, "-h") // #nosec G204 -- the uninstaller the install recorded
	cmd.Stdout, cmd.Stderr = &out, &out
	_ = cmd.Run() // -h exits 2
	if strings.Contains(out.String(), "-upgrade") {
		return []string{"--upgrade", "--verbose"}
	}
	return []string{"--quiet"}
}

// runOldUninstaller runs the installed uninstaller for an upgrade and
// reports its output.
func runOldUninstaller(ctx context.Context, path string, report engine.Reporter) error {
	cmd := exec.CommandContext(ctx, path, oldUninstallArgs(ctx, path)...) // #nosec G204 -- the uninstaller the install recorded
	r, w := io.Pipe()
	cmd.Stdout, cmd.Stderr = w, w
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			report(engine.Event{Kind: engine.Detail, Text: sc.Text(), Step: -1})
		}
		_, _ = io.Copy(io.Discard, r)
	}()
	err := cmd.Run()
	_ = w.Close()
	wg.Wait()
	if err != nil {
		return fmt.Errorf("the installed version's uninstaller failed, so nothing new was installed; run %s to finish removing it: %w", path, err)
	}
	return nil
}

// lock is the install lock, which an upgrade lets go of while the old
// uninstaller, which takes the same lock, runs.
type lock struct {
	id, scope string
	getenv    func(string) string
	release   func()
}

func takeLock(id, scope string, getenv func(string) string) (*lock, error) {
	l := &lock{id: id, scope: scope, getenv: getenv}
	return l, l.take()
}

func (l *lock) take() error {
	release, err := engine.Lock(l.id, l.scope, l.getenv)
	if err != nil {
		return err //nolint:wrapcheck // engine's error says what is locked
	}
	l.release = release
	return nil
}

func (l *lock) Release() {
	if l.release != nil {
		l.release()
		l.release = nil
	}
}

// replace removes the installed version through its own uninstaller,
// plans again, checks the plan is the one shown, and installs. l is held
// on entry and on return.
func replace(ctx context.Context, shown *engine.Plan, old *installed, p Payload, getenv func(string) string, l *lock, report engine.Reporter) error {
	report(engine.Event{Kind: engine.Step, Text: engine.StepReplace, Step: 0})
	l.Release()
	err := runOldUninstaller(ctx, old.ix.Uninstaller, report)
	if lerr := l.take(); err == nil {
		err = lerr
	}
	if err != nil {
		return err
	}
	fresh, err := engine.NewPlan(shown.Manifest, engine.Options{
		Scope: shown.Scope, Root: shown.Root, Env: getenv, Uninstaller: p.Uninstaller, Params: shown.Params, Replaces: shown.Replaces,
	})
	if err != nil {
		return fmt.Errorf("%s %s was removed, but the new version cannot be installed: %w", old.receipt.App.Name, old.receipt.App.Version, err)
	}
	a, err := shown.ContentDigest()
	if err != nil {
		return err
	}
	b, err := fresh.ContentDigest()
	if err != nil {
		return err
	}
	if a != b {
		return fmt.Errorf("%s %s was removed, but the install is no longer the one shown, so nothing new was installed; run the installer again",
			old.receipt.App.Name, old.receipt.App.Version)
	}
	if _, err := engine.Apply(ctx, fresh, p.Files, p.Uninstaller, report); err != nil {
		return fmt.Errorf("%s %s was removed, and installing %s failed and was undone: %w",
			old.receipt.App.Name, old.receipt.App.Version, fresh.Manifest.App.Version, err)
	}
	return nil
}

package installer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ushineko/fynstall/engine"
	"github.com/ushineko/fynstall/manifest"
)

/*
System scope runs only the changes as root (spec 001 R13). The installer
and the uninstaller stay the person's own processes: they plan, ask and
draw, then run this same program again as an administrator with
--apply-plan or --apply-uninstall: under pkexec (the window) or sudo (the
command line) on Linux, through a UAC prompt on Windows. That helper
reports one JSON event per line, on its stdout or, on Windows, on a named
pipe, and the parent shows the events as its own.

The helper trusts nothing it is handed but the person's choices. An install
request holds the scope, the directory and the parameter values, and the
digest of the plan the person approved; the helper makes the plan again
from its own embedded payload and refuses when the digest differs (R14).
*/

// request is the plan file the installer hands its helper (R14).
type request struct {
	Scope  string            `json:"scope"`
	Root   string            `json:"root"`
	Params map[string]string `json:"params,omitempty"`
	Digest string            `json:"digest"`
	// Upgrade is true when the plan replaces the install in Scope (R17):
	// the helper runs its uninstaller first.
	Upgrade bool `json:"upgrade,omitempty"`
}

// wire is an event on the helper's stdout.
type wire struct {
	// Kind is step, detail, warn, progress, leftovers, removed, done or
	// error.
	Kind      string            `json:"kind"`
	Text      string            `json:"text,omitempty"`
	Step      int               `json:"step"`
	Counts    *engine.Counts    `json:"counts,omitempty"`
	Leftovers []engine.Leftover `json:"leftovers,omitempty"`
}

var kinds = map[engine.EventKind]string{ //nolint:gochecknoglobals // a fixed table
	engine.Step: "step", engine.Detail: "detail", engine.Warn: "warn", engine.Progress: "progress",
}

// needsElevation reports whether changes in scope need a helper: a system
// install, in a process that is not an administrator's. The tests ask for a
// helper even then (helperForced), so that it runs where they run.
func needsElevation(scope string, getenv func(string) string) bool {
	return scope == "system" && (!privileged() || helperForced(getenv))
}

// helper is a running privileged helper, seen from the parent.
type helper struct {
	ctx context.Context
	// stdin is what the helper reads: closing it stops the helper. wait
	// waits for the helper to end.
	stdin   io.WriteCloser
	wait    func() error
	sc      *bufio.Scanner
	report  engine.Reporter
	stop    func() bool
	done    bool
	failure string
	// removed is true once an uninstall helper says it removed the
	// leftovers.
	removed bool
}

// helperProgram is the program the helper runs: this one. The tests point
// it at a stand-in.
var helperProgram = func() (string, error) { //nolint:gochecknoglobals // a seam for the tests
	self, err := os.Executable()
	if err != nil {
		return "", err //nolint:wrapcheck // wrapped by the caller
	}
	return filepath.EvalSymlinks(self) //nolint:wrapcheck // as above
}

// errDenied is what a helper's wait returns when no administrator allowed
// it to start.
var errDenied = errors.New("an administrator did not allow it; nothing was changed")

// startElevated runs this program as an administrator with args. Its
// events go to report. Cancelling ctx closes the helper's input, which is
// how a person's process stops a privileged one. How the helper is started
// and how the two talk is the platform's (launchHelper).
func startElevated(ctx context.Context, gui bool, getenv func(string) string, stderr io.Writer, args []string, report engine.Reporter) (*helper, error) {
	self, err := helperProgram()
	if err != nil {
		return nil, fmt.Errorf("find this program: %w", err)
	}
	stdin, stdout, wait, err := launchHelper(ctx, gui, getenv, stderr, self, args)
	if err != nil {
		return nil, err
	}
	h := &helper{ctx: ctx, stdin: stdin, wait: wait, report: report}
	h.stop = context.AfterFunc(ctx, func() { _ = h.stdin.Close() })
	h.sc = bufio.NewScanner(stdout)
	h.sc.Buffer(make([]byte, 64<<10), 16<<20)
	return h, nil
}

// next reads events until the helper finishes or reports leftovers. With
// leftovers it returns them, and the helper waits for answer.
func (h *helper) next() []engine.Leftover {
	for h.sc.Scan() {
		var w wire
		if json.Unmarshal(h.sc.Bytes(), &w) != nil {
			h.report(engine.Event{Kind: engine.Detail, Text: h.sc.Text(), Step: -1})
			continue
		}
		switch w.Kind {
		case "done":
			h.done = true
		case "removed":
			h.removed = true
		case "error":
			h.failure = w.Text
		case "leftovers":
			return w.Leftovers
		default:
			ev := engine.Event{Text: w.Text, Step: w.Step}
			for k, name := range kinds {
				if name == w.Kind {
					ev.Kind = k
				}
			}
			if w.Counts != nil {
				ev.Counts = *w.Counts
			}
			h.report(ev)
		}
	}
	return nil
}

// removeLeftovers tells a waiting uninstall helper to remove the
// leftovers, and returns an error unless it says it did: a helper that is
// gone, or one that kept them, must not look like a removal.
func (h *helper) removeLeftovers() error {
	_, werr := fmt.Fprintln(h.stdin, answerRemove)
	err := h.finish()
	switch {
	case err != nil:
		return err
	case werr != nil || !h.removed:
		return errors.New("the files the program made were not removed: the helper did not take the answer")
	}
	return nil
}

// finish closes the helper's stdin, waits for it, and returns how it
// ended.
func (h *helper) finish() error {
	_ = h.stdin.Close()
	h.next()
	h.stop()
	waitErr := h.wait()
	switch {
	case h.failure != "":
		return errors.New(h.failure)
	case h.done && waitErr == nil:
		return nil
	case errors.Is(waitErr, errDenied):
		return errDenied
	case errors.Is(h.ctx.Err(), context.Canceled):
		return fmt.Errorf("stopped: %w", h.ctx.Err())
	}
	return fmt.Errorf("the privileged helper stopped before it finished (%w); what it did was undone where it could be", waitErr)
}

// runElevated runs a helper to the end.
func runElevated(ctx context.Context, gui bool, e Env, args []string, report engine.Reporter) error {
	h, err := startElevated(ctx, gui, e.Getenv, e.Err, args, report)
	if err != nil {
		return err
	}
	h.next()
	return h.finish()
}

// answerRemove is the line that tells a waiting uninstall helper to remove
// the leftovers; anything else, or the end of its input, keeps them.
const answerRemove = "remove-leftovers"

// helperOut writes the helper's events as JSON lines.
type helperOut struct{ enc *json.Encoder }

func newHelperOut(w io.Writer) helperOut { return helperOut{enc: json.NewEncoder(w)} }

func (h helperOut) send(w wire) { _ = h.enc.Encode(w) }

func (h helperOut) report(ev engine.Event) {
	w := wire{Kind: kinds[ev.Kind], Text: ev.Text, Step: ev.Step}
	if ev.Kind == engine.Progress {
		c := ev.Counts
		w.Counts = &c
	}
	h.send(w)
}

func (h helperOut) fail(err error) int {
	h.send(wire{Kind: "error", Text: err.Error()})
	return exitFail
}

// cancelOnEOF returns a context that ends when in closes: the parent's
// way of stopping the helper.
func cancelOnEOF(in io.Reader) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_, _ = io.Copy(io.Discard, in)
		cancel()
	}()
	return ctx, cancel
}

// writeRequest writes req to a new file only this user can read, in a new
// directory only this user can write, and returns the file and its
// removal (R14).
func writeRequest(req request) (string, func(), error) {
	dir, err := os.MkdirTemp("", "fynstall-plan-*")
	if err != nil {
		return "", nil, fmt.Errorf("plan file: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	b, err := json.Marshal(req)
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "plan.json"), b, 0o600)
	}
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("plan file: %w", err)
	}
	return filepath.Join(dir, "plan.json"), cleanup, nil
}

// applyRequest makes and applies plan for the elevated installer: it
// writes the request, runs the helper, and reports its events.
func applyRequest(ctx context.Context, plan *engine.Plan, gui bool, e Env, report engine.Reporter) error {
	digest, err := plan.Digest()
	if err != nil {
		return err
	}
	file, cleanup, err := writeRequest(request{Scope: plan.Scope, Root: plan.Root, Params: plan.Params, Digest: digest, Upgrade: plan.Replaces != ""})
	if err != nil {
		return err
	}
	defer cleanup()
	return runElevated(ctx, gui, e, []string{"--apply-plan", file}, report)
}

// applyPlan is the install helper: --apply-plan <file>.
func applyPlan(file string, m *manifest.Manifest, p Payload, e Env) int {
	out := newHelperOut(e.Out)
	b, err := os.ReadFile(file) // #nosec G304 -- the plan file the person's installer wrote; only its choices are used
	if err != nil {
		return out.fail(fmt.Errorf("read the plan: %w", err))
	}
	var req request
	if err := json.Unmarshal(b, &req); err != nil {
		return out.fail(fmt.Errorf("read the plan: %w", err))
	}
	l, err := takeLock(m.App.ID, req.Scope, e.Getenv)
	if err != nil {
		return out.fail(err)
	}
	defer l.Release()
	o := engine.Options{Scope: req.Scope, Root: req.Root, Env: e.Getenv, Uninstaller: p.Uninstaller, Params: req.Params}
	var old *installed
	if req.Upgrade {
		ix, _, err := engine.ReadIndex(m, req.Scope, e.Getenv)
		if err == nil {
			old, err = readExisting(ix)
		}
		if err != nil {
			return out.fail(fmt.Errorf("find the installed version: %w", err))
		}
		o.Replacing = old.receipt
		if o.Params == nil {
			o.Params = map[string]string{}
		}
		// The secrets the person's process could not read, read as root.
		previous, _ := engine.Previous(m, old.receipt, e.Getenv)
		for _, d := range m.Parameters {
			if v, ok := previous[d.Name]; ok && d.Secret && o.Params[d.Name] == "" {
				o.Params[d.Name] = v
			}
		}
	}
	plan, err := engine.NewPlan(m, o)
	if err != nil {
		return out.fail(err)
	}
	digest, err := plan.Digest()
	if err != nil {
		return out.fail(err)
	}
	if digest != req.Digest {
		return out.fail(errors.New("the install is not the one that was shown: something changed since, or the plan file was edited. Nothing was changed; run the installer again"))
	}
	ctx, cancel := cancelOnEOF(e.In)
	defer cancel()
	if old != nil {
		if err := replace(ctx, plan, old, p, e.Getenv, l, out.report); err != nil {
			return out.fail(err)
		}
	} else if _, err := engine.Apply(ctx, plan, p.Files, p.Uninstaller, out.report); err != nil {
		return out.fail(fmt.Errorf("install failed, and the changes were undone: %w", err))
	}
	out.send(wire{Kind: "done"})
	return exitOK
}

// applyUninstall is the uninstall helper: --apply-uninstall. It removes
// the install r. With removeLeftovers it removes the leftovers too;
// otherwise it reports them and waits for the parent's answer, so the
// person is asked once for an administrator, not twice.
func applyUninstall(r *engine.Receipt, removeLeftovers bool, why engine.Reason, e Env) int {
	out := newHelperOut(e.Out)
	unlock, err := engine.Lock(r.App.ID, r.Scope, e.Getenv)
	if err != nil {
		return out.fail(err)
	}
	defer unlock()
	left, err := engine.Uninstall(r, why, out.report)
	if err != nil {
		return out.fail(err)
	}
	// An upgrade leaves the leftovers where they are, without asking: the
	// new version installs among them.
	if len(left) > 0 && !removeLeftovers && why != engine.ReasonUpgrade {
		out.send(wire{Kind: "leftovers", Leftovers: left})
		line, _ := bufio.NewReader(e.In).ReadString('\n')
		removeLeftovers = strings.TrimSpace(line) == answerRemove
	}
	if len(left) > 0 && removeLeftovers {
		if err := engine.RemoveLeftovers(r, left, out.report); err != nil {
			return out.fail(err)
		}
		out.send(wire{Kind: "removed"})
	}
	out.send(wire{Kind: "done"})
	return exitOK
}

package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ushineko/fynstall/manifest"
	"github.com/ushineko/fynstall/platform"
)

// PlannedAction is one service, run or migrate action Apply will take, in
// config order (spec 002 D2a). Exactly one field is set.
type PlannedAction struct {
	Service *PlannedService
	Run     *PlannedRun
	Migrate *PlannedMigrate
}

// PlannedService is a service action: Unit is the unit file Apply writes
// with Content.
type PlannedService struct {
	Name    string
	Unit    string
	Content []byte
	Start   bool
	// Exists is true when a unit file is already at Unit. Apply saves it,
	// and the uninstaller puts it back with its enabled and running state.
	Exists bool
}

// PlannedRun is a run action, or an uninstall hook: Exec is absolute and
// runs in Dir with Args.
type PlannedRun struct {
	Exec string
	Dir  string
	Args []string
	// Undo are the arguments that undo it; NoUndo says the config chose
	// none.
	Undo   []string
	NoUndo bool
}

// PlannedMigrate moves From to To. Present is false when there is nothing
// at From, and Apply then does nothing.
type PlannedMigrate struct {
	From, To string
	Present  bool
}

// Hook is an uninstall hook (a run action with on: uninstall): it runs
// before the uninstaller removes anything. It is kept in the receipt.
type Hook struct {
	Exec            string   `json:"exec"`
	Dir             string   `json:"dir"`
	Args            []string `json:"args,omitempty"`
	ContinueOnError bool     `json:"continue_on_error,omitempty"`
}

// ServiceEntry is what an OpService entry needs to put a service back.
type ServiceEntry struct {
	Name string `json:"name"`
	// WasEnabled and WasActive are the state of a service of the same
	// name that was there before the install.
	WasEnabled bool `json:"was_enabled,omitempty"`
	WasActive  bool `json:"was_active,omitempty"`
}

// RunEntry is how an OpRun entry is undone: Exec run in Dir with Undo.
type RunEntry struct {
	Exec string   `json:"exec"`
	Dir  string   `json:"dir"`
	Undo []string `json:"undo,omitempty"`
}

// addActions plans the manifest's service, run and migrate actions. The
// arguments may use the path placeholders and non-secret parameters.
func (p *Plan) addActions(vars, params map[string]string) error {
	all := map[string]string{}
	for k, v := range vars {
		all[k] = v
	}
	for _, d := range p.Manifest.Parameters {
		if !d.Secret {
			all["param:"+d.Name] = params[d.Name]
		}
	}
	expandAll := func(args []string) ([]string, error) {
		out := make([]string, len(args))
		for i, a := range args {
			v, err := manifest.Expand(a, all)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	}
	for _, a := range p.Manifest.Actions {
		switch {
		case a.Service != nil:
			if err := p.addService(a.Service, vars, expandAll); err != nil {
				return err
			}
		case a.Run != nil:
			r := a.Run
			args, err := expandAll(r.Args)
			if err != nil {
				return fmt.Errorf("run %s: %w", r.Exec, err)
			}
			if r.Hook {
				p.Hooks = append(p.Hooks, Hook{Exec: p.inRoot(r.Exec), Dir: p.Root, Args: args, ContinueOnError: r.ContinueOnError})
				continue
			}
			undo, err := expandAll(r.Undo)
			if err != nil {
				return fmt.Errorf("run %s: %w", r.Exec, err)
			}
			p.Actions = append(p.Actions, PlannedAction{Run: &PlannedRun{
				Exec: p.inRoot(r.Exec), Dir: p.Root, Args: args, Undo: undo, NoUndo: r.NoUndo,
			}})
		case a.Migrate != nil:
			if err := p.addMigrate(a.Migrate, vars); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Plan) addService(s *manifest.Service, vars map[string]string, expandAll func([]string) ([]string, error)) error {
	if !platform.HasServiceManager() {
		return fmt.Errorf("service %s: systemctl is not on PATH, and this install runs a service", s.Name)
	}
	args, err := expandAll(s.Args)
	if err != nil {
		return fmt.Errorf("service %s: %w", s.Name, err)
	}
	desc := s.Description
	if desc == "" {
		desc = p.Manifest.App.Name
	}
	ps := &PlannedService{
		Name: s.Name, Unit: platform.UnitPath(vars, s.Name), Start: s.Start,
		Content: platform.RenderUnit(platform.Unit{
			AppID: p.Manifest.App.ID, Description: desc, Exec: p.inRoot(s.Exec), Args: args, Restart: s.Restart,
		}),
	}
	if ps.Exists, err = p.check(ps.Unit, vars["config"]); err != nil {
		return err
	}
	p.Actions = append(p.Actions, PlannedAction{Service: ps})
	return nil
}

func (p *Plan) addMigrate(m *manifest.Migrate, vars map[string]string) error {
	var paths [2]string
	for i, tmpl := range []string{m.From, m.To} {
		base, _, _ := strings.Cut(tmpl, "/")
		v, err := manifest.Expand(tmpl, vars)
		if err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		v = filepath.Clean(v)
		if err := Contained(vars[strings.Trim(base, "{}")], v); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		paths[i] = v
	}
	pm := &PlannedMigrate{From: paths[0], To: paths[1]}
	if _, err := os.Lstat(pm.From); err == nil {
		pm.Present = true
		if _, err := os.Lstat(pm.To); err == nil {
			return fmt.Errorf("migrate: both %s and %s exist; move one of them first", pm.From, pm.To)
		}
	}
	p.Actions = append(p.Actions, PlannedAction{Migrate: pm})
	return nil
}

// apply takes one planned action. Each is journalled before its effect, so
// a failure part-way is undone too.
func (j *journal) apply(ctx context.Context, a PlannedAction) error {
	switch {
	case a.Service != nil:
		return j.service(a.Service)
	case a.Run != nil:
		r := a.Run
		if !r.NoUndo {
			j.add(Entry{Op: OpRun, Path: r.Exec, Run: &RunEntry{Exec: r.Exec, Dir: r.Dir, Undo: r.Undo}})
		}
		j.report.emit(Detail, "run %s", strings.Join(append([]string{r.Exec}, r.Args...), " "))
		return runProgram(ctx, r.Exec, r.Dir, r.Args, j.report)
	case a.Migrate != nil:
		m := a.Migrate
		if !m.Present {
			j.report.emit(Detail, "nothing to move from %s", m.From)
			return nil
		}
		if err := os.Rename(m.From, m.To); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		j.add(Entry{Op: OpMigrate, Path: m.To, From: m.From})
		j.report.emit(Detail, "moved %s to %s", m.From, m.To)
	}
	return nil
}

func (j *journal) service(s *PlannedService) error {
	e := Entry{Op: OpService, Path: s.Unit, Service: &ServiceEntry{Name: s.Name}}
	if s.Exists {
		e.Service.WasEnabled, e.Service.WasActive = platform.ServiceState(s.Name)
		if err := j.save(s.Unit, &e); err != nil {
			return fmt.Errorf("back up %s: %w", s.Unit, err)
		}
	}
	if err := atomicWrite(s.Unit, strings.NewReader(string(s.Content)), "", 0o644); err != nil {
		return err
	}
	j.add(e)
	j.report.emit(Detail, "service %s (%s)", s.Name, s.Unit)
	if err := platform.ReloadServices(); err != nil {
		return err
	}
	if err := platform.EnableService(s.Name); err != nil {
		return err
	}
	if s.Start {
		return platform.RestartService(s.Name)
	}
	return nil
}

// undoAction reverses an OpService, OpRun or OpMigrate entry. What the
// service manager or a program says is a warning: the files are what the
// journal can put back for certain, and a stuck program must not keep an
// uninstall from finishing.
func (j *journal) undoAction(e Entry) error {
	switch e.Op {
	case OpService:
		s := e.Service
		if err := platform.DisableService(s.Name); err != nil {
			j.report.emit(Warn, "%v", err)
		}
		var err error
		if e.Backup != "" || e.OldLink != "" {
			err = j.restore(e)
		} else if err = os.Remove(e.Path); errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
		if err != nil {
			return err
		}
		j.report.emit(Detail, "removed service %s", s.Name)
		if err := platform.ReloadServices(); err != nil {
			j.report.emit(Warn, "%v", err)
		}
		if s.WasEnabled {
			if err := platform.EnableService(s.Name); err != nil {
				j.report.emit(Warn, "%v", err)
			}
		}
		if s.WasActive {
			if err := platform.StartService(s.Name); err != nil {
				j.report.emit(Warn, "%v", err)
			}
		}
	case OpRun:
		r := e.Run
		j.report.emit(Detail, "run %s", strings.Join(append([]string{r.Exec}, r.Undo...), " "))
		if err := runProgram(context.Background(), r.Exec, r.Dir, r.Undo, j.report); err != nil {
			j.report.emit(Warn, "undo: %v", err)
		}
	case OpMigrate:
		if err := os.Rename(e.Path, e.From); err != nil {
			return fmt.Errorf("move %s back to %s: %w", e.Path, e.From, err)
		}
		j.report.emit(Detail, "moved %s back to %s", e.Path, e.From)
	}
	return nil
}

// runHooks runs the uninstall hooks, before anything is removed. A
// failure stops the uninstall unless the hook may fail.
func runHooks(hooks []Hook, report Reporter) error {
	for _, h := range hooks {
		report.emit(Detail, "run %s", strings.Join(append([]string{h.Exec}, h.Args...), " "))
		if err := runProgram(context.Background(), h.Exec, h.Dir, h.Args, report); err != nil {
			if !h.ContinueOnError {
				return fmt.Errorf("uninstall hook: %w; nothing was removed", err)
			}
			report.emit(Warn, "uninstall hook: %v; going on, as the install allows", err)
		}
	}
	return nil
}

// outputTail is how many of a failed program's last output lines its error
// carries.
const outputTail = 10

// runProgram runs a payload program, never a shell, in dir. Each line of
// its output is reported; a failure carries the last lines.
func runProgram(ctx context.Context, path, dir string, args []string, report Reporter) error {
	cmd := exec.CommandContext(ctx, path, args...) // #nosec G204 -- a payload program the config declares, shown before it runs
	cmd.Dir = dir
	r, w := io.Pipe()
	cmd.Stdout, cmd.Stderr = w, w
	var tail []string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			report.emit(Detail, "  %s", sc.Text())
			tail = append(tail, sc.Text())
			if len(tail) > outputTail {
				tail = tail[1:]
			}
		}
		_, _ = io.Copy(io.Discard, r)
	}()
	err := cmd.Run()
	_ = w.Close()
	wg.Wait()
	if err != nil {
		msg := fmt.Sprintf("%s: %v", strings.Join(append([]string{filepath.Base(path)}, args...), " "), err)
		if len(tail) > 0 {
			msg += ": " + strings.Join(tail, " / ")
		}
		return errors.New(msg)
	}
	return nil
}

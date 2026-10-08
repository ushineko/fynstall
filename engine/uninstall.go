package engine

import "fmt"

// Uninstall replays r's journal in reverse: it removes what the install
// created, puts back what it replaced, then removes the receipt and the
// directories the install created that are now empty (R9d). Paths in
// r.Keep are never touched.
//
// It can be run again after a failure: a file already removed or restored
// is skipped, and the receipt stays until every file is dealt with.
func Uninstall(r *Receipt, report Reporter) error {
	j := &journal{root: r.Root, entries: r.Journal, report: report}
	report.emit(Step, "Removing %s %s", r.App.Name, r.App.Version)
	if err := j.undo(r.Keep); err != nil {
		return fmt.Errorf("uninstall %s: %w", r.App.Name, err)
	}
	return nil
}

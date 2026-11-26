package runner

import (
	"os"
	"path/filepath"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/stamp"
)

// prepareSnapshot materializes the frame, shared word files and word directories captured in
// stamps into a private directory inside this run's own record directory so every executed frame
// step and word runs the exact bytes that were stamped. It also waits out the coarse filesystem
// timestamp tick window so an edit-and-restore during execution cannot share the pre-run ctime.
func (r *Run) prepareSnapshot(stamps []stamp.Stamp) (func(), error) {
	noop := func() {}
	if r.trial || len(stamps) == 0 {
		return noop, nil
	}
	snapDir := filepath.Join(r.record.Dir, ".snap")
	cleanup := func() {
		r.project.FrameDir = ""
		r.snapWords = ""
		_ = os.RemoveAll(snapDir)
	}
	if binding := stamps[0].Binding(); binding != nil {
		frameDir := filepath.Join(snapDir, "frame")
		if err := binding.MaterializeFrame(frameDir); err != nil {
			cleanup()
			return noop, err
		}
		wordsDir := filepath.Join(snapDir, "words")
		if err := binding.MaterializeShared(wordsDir); err != nil {
			cleanup()
			return noop, err
		}
		r.project.FrameDir = frameDir
		r.snapWords = wordsDir
	}
	seen := map[string]bool{}
	for i, step := range r.steps {
		if i >= len(stamps) || seen[step.Word.Name] {
			continue
		}
		wd := stamps[i].WordDir()
		if wd == nil {
			continue
		}
		if r.snapWords == "" {
			r.snapWords = filepath.Join(snapDir, "words")
		}
		if err := wd.Materialize(filepath.Join(r.snapWords, step.Word.Name)); err != nil {
			cleanup()
			return noop, err
		}
		seen[step.Word.Name] = true
	}
	stamp.WaitTick(stamps)
	return cleanup, nil
}

// wordRun returns the snapshotted run script for step when materialized, falling back to step's
// original path (for example in onboarding trials whose steps already point at their snapshot).
func (r *Run) wordRun(step dictionary.Step) string {
	if r.snapWords != "" {
		candidate := filepath.Join(r.snapWords, step.Word.Name, "run")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return step.Word.Run()
}

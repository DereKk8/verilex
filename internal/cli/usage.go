package cli

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/runner"
	"github.com/DereKk8/verilex/internal/usage"
	"github.com/DereKk8/verilex/internal/verdict"
)

type usageCapture struct {
	store   usage.Store
	entry   usage.Entry
	started time.Time
}

func usageStore() usage.Store {
	dir := os.Getenv("VERILEX_USAGE_LEDGER")
	if dir == "" {
		dir = filepath.Join(runner.StateHome(), "usage")
	}
	return usage.Store{Dir: dir}
}

func usageCommand(args options, out io.Writer, refuse func(error) int) int {
	store := usageStore()
	if len(args.operands) > 0 {
		if err := store.Annotate(args.operands[0], args.tokens, args.miss); err != nil {
			return refuse(err)
		}
		fmt.Fprintf(out, "usage: recorded caller data for %s\n", args.operands[0])
		return 0
	}
	result, err := store.Read()
	if err != nil {
		return refuse(err)
	}
	if args.json {
		if err = encode(out, result); err != nil {
			return refuse(err)
		}
	} else {
		t := result.Totals
		tokens := fmt.Sprintf("%d", t.Tokens)
		if t.TokensSaturated {
			tokens = ">=" + tokens
		}
		fmt.Fprintf(out, "usage: %d runs; green %d, red %d, inconclusive %d, unfinished %d; misses %d; %.3fs; tokens %s (%d runs unknown, %d runs estimated)\n", t.Runs, t.Green, t.Red, t.Inconclusive, t.Unfinished, t.Misses, t.Seconds, tokens, t.TokensUnknown, t.TokensEstimated)
		fmt.Fprintf(out, "ledger: %s\n", store.Dir)
	}
	return 0
}

func newUsage(project dictionary.Project, args options) *usageCapture {
	c := &usageCapture{store: usageStore(), started: time.Now(), entry: usage.Entry{Change: args.change, Tokens: args.tokens}}
	// Change identity is optional. Bound both Git's work and the bytes hashed,
	// including when a skipped run sits next to a huge tracked binary change.
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	revision := exec.CommandContext(ctx, "git", "-C", project.Root, "rev-parse", "HEAD")
	if data, err := revision.Output(); err == nil {
		c.entry.Revision = strings.TrimSpace(string(data))
		diff := exec.CommandContext(ctx, "git", "-C", project.Root, "diff", "HEAD", "--no-ext-diff", "--no-textconv", "--binary", "--", ".")
		pipe, err := diff.StdoutPipe()
		if err != nil || diff.Start() != nil {
			return c
		}
		digest := sha256.New()
		const maxDiff = 1 << 20
		n, copyErr := io.Copy(digest, io.LimitReader(pipe, maxDiff+1))
		if n > maxDiff || copyErr != nil {
			_ = diff.Process.Kill()
		}
		if err := diff.Wait(); err == nil && copyErr == nil && n <= maxDiff && ctx.Err() == nil {
			c.entry.DiffSHA256 = fmt.Sprintf("%x", digest.Sum(nil))
		}
	}
	return c
}

func (c *usageCapture) save(stderr io.Writer) {
	if err := c.store.Save(c.entry); err != nil {
		// Recording trouble is not a different product verdict. Never hide the lost data.
		fmt.Fprintf(stderr, "verilex: usage ledger write failed: %v\n", err)
	}
}

func (c *usageCapture) begin(record runner.Record, stderr io.Writer) {
	c.entry.Record = record
	c.save(stderr)
}

func (c *usageCapture) finish(record runner.Record, stderr io.Writer) {
	if record.Run == "" {
		if err := c.store.Remove(c.entry.Record.Run); err != nil {
			fmt.Fprintf(stderr, "verilex: usage ledger removal failed: %v\n", err)
		}
		return
	}
	c.entry.Record = record
	seconds := time.Since(c.started).Seconds()
	c.entry.Seconds = &seconds
	c.save(stderr)
}

// A request with no covering claim still contributes an inconclusive run to
// usage, but does not launch an instance or add a proof-stamp run.
func (c *usageCapture) uncovered(project dictionary.Project, args options, reason string, stderr io.Writer) (string, error) {
	id, err := usage.ID()
	if err != nil {
		return "", err
	}
	v := verdict.Inconclusive
	record := runner.Record{Run: id, Project: project.Name, Root: project.Root, Started: c.started.UTC().Format(time.RFC3339Nano), Finished: time.Now().UTC().Format(time.RFC3339Nano), Verdict: &v, Reason: &reason, Requested: requested(args), Cleanup: "none", Words: []runner.WordRecord{}}
	c.finish(record, stderr)
	return id, nil
}

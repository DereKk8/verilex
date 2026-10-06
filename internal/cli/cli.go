// Package cli exposes the verilex command line with explicit output streams.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/lifecycle"
	"github.com/DereKk8/verilex/internal/report"
	"github.com/DereKk8/verilex/internal/runner"
	"github.com/DereKk8/verilex/internal/ticket"
	"github.com/DereKk8/verilex/internal/verdict"
)

const usage = "usage: verilex [-h] [--project PROJECT] {run,plan,ticket,words,claims,runs,cleanup,new,propose,admit,gap,check} ...\n"

type options struct {
	project, command, operand, verdict, from, ticket string
	implements                                       []string
	keep, fresh, json                                bool
}

func Main(argv []string, out, stderr io.Writer) int {
	args, help, err := parse(argv)
	if help {
		printHelp(args.command, out)
		return 0
	}
	refuse := func(err error) int { fmt.Fprintf(stderr, "verilex: refused: %v\n", err); return 2 }
	if err != nil {
		fmt.Fprint(stderr, usageFor(args.command))
		name := "verilex"
		if args.command != "" {
			name += " " + args.command
		}
		fmt.Fprintf(stderr, "%s: error: %v\n", name, err)
		return 2
	}
	root, err := filepath.Abs(args.project)
	if err != nil {
		return refuse(err)
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if args.command == "new" {
		return scaffold(root, args, out, refuse)
	}
	project, err := dictionary.FindProject(root)
	if err != nil {
		return refuse(err)
	}
	switch args.command {
	case "propose", "admit", "gap", "check":
		return curate(project, args, out, refuse)
	case "runs":
		records, err := runner.LoadRuns(project)
		if err != nil {
			return refuse(err)
		}
		for _, record := range records {
			status := "running"
			if record.Verdict != nil {
				status = string(*record.Verdict)
			}
			fmt.Fprintf(out, "%s  %s  cleanup=%s  %s\n", record.Run, status, record.Cleanup, record.Chain)
		}
		return 0
	case "cleanup":
		return cleanup(project, args.operand, out, refuse)
	case "claims":
		return claims(project, args.json, out, refuse)
	case "ticket":
		args.ticket = args.operand
	}
	var resolved *ticket.Ticket
	if args.ticket != "" {
		t, err := ticket.Resolve(args.ticket, ticket.Sources{Project: ticket.ProjectFile(project.Dir()), User: ticket.UserFile()})
		if err != nil {
			return refuse(err)
		}
		resolved = &t
	}
	if args.command == "ticket" {
		if args.json {
			if err = encode(out, resolved); err != nil {
				return refuse(err)
			}
		} else {
			report.Ticket(*resolved, out)
		}
		return 0
	}
	words, err := lifecycle.LoadWords(project)
	if err != nil {
		return refuse(err)
	}
	if args.command == "words" {
		for _, word := range words {
			fmt.Fprintln(out, strings.TrimSpace(word.Name+" "+strings.Join(word.Args, " ")))
			fmt.Fprintf(out, "  promise:  %s\n", word.Promise)
			if word.Claim != nil {
				fmt.Fprintf(out, "  claim:    %s via %s\n", word.Proves(), word.Entry)
			}
			fmt.Fprintf(out, "  requires: %s  provides: %s\n", states(word.Requires), states(word.Provides))
			status, err := lifecycle.StatusOf(project, word)
			if err != nil {
				return refuse(err)
			}
			fmt.Fprintf(out, "  status:   %s\n", status.State)
		}
		return 0
	}
	steps, err := dictionary.ParseChain(args.operand, words)
	if err != nil {
		return refuse(err)
	}
	if err = dictionary.CheckOrder(steps); err != nil {
		return refuse(err)
	}
	opts := runner.Options{Keep: args.keep, Fresh: args.fresh, Continue: args.from, Ticket: resolved}
	plan, err := runner.Decide(project, steps, opts)
	if err != nil {
		return refuse(err)
	}
	if args.command == "plan" {
		if args.json {
			if err = encode(out, plan); err != nil {
				return refuse(err)
			}
		} else {
			report.Plan(plan, out)
		}
		return 0
	}
	run, err := runner.New(project, args.operand, steps)
	if err != nil {
		return refuse(err)
	}
	record, runErr := run.Execute(plan, opts)
	if args.json {
		if err = encode(out, record); err != nil {
			return refuse(err)
		}
	} else {
		report.Human(record, out)
	}
	if runErr != nil {
		fmt.Fprintf(stderr, "verilex: %v\n", runErr)
		return 2
	}
	if record.Verdict == nil {
		return verdict.Inconclusive.ExitCode()
	}
	return record.Verdict.ExitCode()
}

func encode(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

// command describes one subcommand's operand and flags.
type command struct {
	operand, usage, help string
	flags                []string // boolean flags
	values               []string // flags taking a value
}

var commands = map[string]command{
	"run":     {"chain", "[--keep] [--fresh] [--continue RUN] [--ticket FILE] [--json] chain", "run a chain of words, e.g. 'a | b X | c'", []string{"--keep", "--fresh", "--json"}, []string{"--continue", "--ticket"}},
	"plan":    {"chain", "[--continue RUN] [--ticket FILE] [--json] chain", "show whether a chain would be skipped or run live, running nothing", []string{"--json"}, []string{"--continue", "--ticket"}},
	"ticket":  {"file", "[--json] file", "validate a run ticket and resolve its profile and defaults", []string{"--json"}, nil},
	"words":   {"", "", "list the dictionary and each word's lifecycle status", nil, nil},
	"claims":  {"", "[--json]", "list each claim's current version, the words that prove it, and any review it needs", []string{"--json"}, nil},
	"runs":    {"", "", "list this project's runs and any instance still alive", nil, nil},
	"cleanup": {"run", "run", "tear down a kept run's instance", nil, nil},
	"new":     {"word", "--implements REF [--implements REF ...] word", "scaffold a provisional word", nil, []string{"--implements"}},
	"propose": {"word", "word", "build a curator packet for a word used in two runs", nil, nil},
	"admit":   {"word", "--verdict FILE word", "record an outside curator's admit or reject verdict", nil, []string{"--verdict"}},
	"gap":     {"description", "description", "record a product moment the feature map has no section for", nil, nil},
	"check":   {"", "", "report admitted words whose files, claim or feature-map sections changed (drift-suspect)", nil, nil},
}

var order = []string{"run", "plan", "ticket", "words", "claims", "runs", "cleanup", "new", "propose", "admit", "gap", "check"}

func parse(argv []string) (options, bool, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return options{}, false, err
	}
	o := options{project: cwd}
	for len(argv) > 0 && strings.HasPrefix(argv[0], "-") {
		arg := argv[0]
		argv = argv[1:]
		if arg == "-h" || arg == "--help" {
			return o, true, nil
		}
		if strings.HasPrefix(arg, "--project=") {
			o.project = strings.TrimPrefix(arg, "--project=")
			continue
		}
		if arg == "--project" && len(argv) > 0 {
			o.project = argv[0]
			argv = argv[1:]
			continue
		}
		if arg == "--project" {
			return o, false, fmt.Errorf("argument --project: expected one argument")
		}
		return o, false, fmt.Errorf("unrecognized arguments: %s", arg)
	}
	if len(argv) == 0 {
		return o, false, fmt.Errorf("the following arguments are required: command")
	}
	o.command = argv[0]
	argv = argv[1:]
	spec, ok := commands[o.command]
	if !ok {
		name := o.command
		o.command = ""
		return o, false, fmt.Errorf("argument command: invalid choice: '%s' (choose from '%s')", name, strings.Join(order, "', '"))
	}
	pos := []string{}
	literal := false
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if !literal && arg == "--" {
			literal = true
			continue
		}
		if !literal && (arg == "-h" || arg == "--help") {
			return o, true, nil
		}
		if !literal && slices.Contains(spec.flags, arg) {
			o.keep = o.keep || arg == "--keep"
			o.fresh = o.fresh || arg == "--fresh"
			o.json = o.json || arg == "--json"
			continue
		}
		if name, value, inline := strings.Cut(arg, "="); !literal && slices.Contains(spec.values, name) {
			if !inline {
				if i+1 == len(argv) {
					return o, false, fmt.Errorf("argument %s: expected one argument", name)
				}
				i++
				value = argv[i]
			}
			switch name {
			case "--implements":
				o.implements = append(o.implements, value)
			case "--continue":
				o.from = value
			case "--ticket":
				if value == "" {
					return o, false, fmt.Errorf("argument --ticket: expected a ticket file")
				}
				o.ticket = value
			default:
				o.verdict = value
			}
			continue
		}
		if !literal && strings.HasPrefix(arg, "-") && arg != "-" {
			return o, false, fmt.Errorf("unrecognized arguments: %s", arg)
		}
		pos = append(pos, arg)
	}
	n := 0
	if spec.operand != "" {
		n = 1
	}
	if len(pos) != n {
		if len(pos) == 0 && n == 1 {
			return o, false, fmt.Errorf("the following arguments are required: %s", spec.operand)
		}
		return o, false, fmt.Errorf("unrecognized arguments: %s", strings.Join(pos[n:], " "))
	}
	if n == 1 {
		o.operand = pos[0]
	}
	if o.command == "admit" && o.verdict == "" {
		return o, false, fmt.Errorf("the following arguments are required: --verdict")
	}
	return o, false, nil
}

func usageFor(name string) string {
	spec, ok := commands[name]
	if !ok {
		return usage
	}
	return strings.TrimSpace("usage: verilex "+name+" [-h] "+spec.usage) + "\n"
}

func printHelp(name string, out io.Writer) {
	fmt.Fprint(out, usageFor(name))
	spec, ok := commands[name]
	if !ok {
		fmt.Fprintf(out, "\nverilex command line.\n\npositional arguments:\n  {%s}\n", strings.Join(order, ","))
		for _, c := range order {
			fmt.Fprintf(out, "    %-20s%s\n", c, commands[c].help)
		}
		fmt.Fprint(out, "\noptions:\n  -h, --help            show this help message and exit\n  --project PROJECT     product checkout (default: cwd)\n")
		return
	}
	fmt.Fprintf(out, "\n%s\n", spec.help)
	if spec.operand != "" {
		fmt.Fprintf(out, "\npositional arguments:\n  %s\n", spec.operand)
	}
	fmt.Fprint(out, "\noptions:\n  -h, --help  show this help message and exit\n")
	for _, flag := range append(append([]string{}, spec.flags...), spec.values...) {
		fmt.Fprintf(out, "  %s\n", flagHelp[flag])
	}
}

var flagHelp = map[string]string{
	"--keep":       "--keep      skip cleanup; tear down later with `verilex cleanup`",
	"--fresh":      "--fresh     run live even when every proof stamp matches",
	"--json":       "--json      print the complete record as JSON",
	"--continue":   "--continue RUN    use the instance RUN kept: run refreshes it, asks the doctor, then runs only the words it does not already prove",
	"--ticket":     "--ticket FILE     the run ticket: intent or diff, profile, harness, model, effort and budgets; see `verilex ticket`",
	"--implements": "--implements REF  the feature-map section the word implements, <skill>/<file>#<section>; repeatable",
	"--verdict":    "--verdict FILE    the curator's verdict JSON for the proposed packet",
}

func cleanup(project dictionary.Project, id string, out io.Writer, refuse func(error) int) int {
	dir := filepath.Join(runner.RunsDir(project), id)
	path := filepath.Join(dir, "run.json")
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return refuse(fmt.Errorf("%s is not a run of %s", id, project.Name))
	}
	record, err := runner.ReadRecord(path)
	if err != nil {
		return refuse(err)
	}
	if record.Cleanup == "done" {
		fmt.Fprintf(out, "verilex: %s was already cleaned up\n", id)
		return 0
	}
	if record.Cleanup == "continued" {
		return refuse(fmt.Errorf("%s was continued by %s; clean up that run instead", id, record.ContinuedBy))
	}
	if record.Cleanup == "none" {
		fmt.Fprintf(out, "verilex: %s launched nothing; it relied on stamps\n", id)
		return 0
	}
	if err = runner.Cleanup(project, &record, dir); err != nil {
		return refuse(err)
	}
	fmt.Fprintf(out, "cleanup: %s\n", record.Cleanup)
	if record.Cleanup == "done" {
		return 0
	}
	return 2
}

func states(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ", ")
}

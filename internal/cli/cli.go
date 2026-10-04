// Package cli exposes the verilex command line with explicit output streams.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/runner"
)

const usage = "usage: verilex [-h] [--project PROJECT] {run,words,runs,cleanup} ...\n"

type options struct {
	project, command, operand string
	keep, json                bool
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
	project, err := dictionary.FindProject(root)
	if err != nil {
		return refuse(err)
	}
	switch args.command {
	case "runs":
		records, err := runner.LoadRuns(project)
		if err != nil {
			return refuse(err)
		}
		for _, record := range records {
			verdict := "running"
			if record.Verdict != nil && *record.Verdict != "" {
				verdict = *record.Verdict
			}
			fmt.Fprintf(out, "%s  %s  cleanup=%s  %s\n", record.Run, verdict, record.Cleanup, record.Chain)
		}
		return 0
	case "cleanup":
		return cleanup(project, args.operand, out, refuse)
	}
	words, err := dictionary.LoadWords(project)
	if err != nil {
		return refuse(err)
	}
	if args.command == "words" {
		for _, word := range words {
			fmt.Fprintln(out, strings.TrimSpace(word.Name+" "+strings.Join(word.Args, " ")))
			fmt.Fprintf(out, "  promise:  %s\n  requires: %s  provides: %s\n", word.Promise, states(word.Requires), states(word.Provides))
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
	run, err := runner.New(project, args.operand, steps)
	if err != nil {
		return refuse(err)
	}
	record, runErr := run.Execute(args.keep)
	if args.json {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		encoder.SetEscapeHTML(false)
		if err = encoder.Encode(record); err != nil {
			return refuse(err)
		}
	} else {
		report(record, out)
	}
	if runErr != nil {
		fmt.Fprintf(stderr, "verilex: %v\n", runErr)
		return 2
	}
	if record.Verdict == nil {
		return 2
	}
	return runner.ExitCode(*record.Verdict)
}

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
	if o.command != "run" && o.command != "words" && o.command != "runs" && o.command != "cleanup" {
		command := o.command
		o.command = ""
		return o, false, fmt.Errorf("argument command: invalid choice: '%s' (choose from 'run', 'words', 'runs', 'cleanup')", command)
	}
	pos := []string{}
	literal := false
	for _, arg := range argv {
		if !literal && arg == "--" {
			literal = true
			continue
		}
		if !literal && (arg == "-h" || arg == "--help") {
			return o, true, nil
		}
		if !literal && o.command == "run" && arg == "--keep" {
			o.keep = true
			continue
		}
		if !literal && o.command == "run" && arg == "--json" {
			o.json = true
			continue
		}
		if !literal && strings.HasPrefix(arg, "-") && arg != "-" {
			return o, false, fmt.Errorf("unrecognized arguments: %s", arg)
		}
		pos = append(pos, arg)
	}
	n := 0
	if o.command == "run" || o.command == "cleanup" {
		n = 1
	}
	if len(pos) != n {
		if len(pos) == 0 && n == 1 {
			operand := "chain"
			if o.command == "cleanup" {
				operand = "run"
			}
			return o, false, fmt.Errorf("the following arguments are required: %s", operand)
		}
		return o, false, fmt.Errorf("unrecognized arguments: %s", strings.Join(pos[n:], " "))
	}
	if n == 1 {
		o.operand = pos[0]
	}
	return o, false, nil
}

func usageFor(command string) string {
	switch command {
	case "run":
		return "usage: verilex run [-h] [--keep] [--json] chain\n"
	case "words", "runs":
		return "usage: verilex " + command + " [-h]\n"
	case "cleanup":
		return "usage: verilex cleanup [-h] run\n"
	default:
		return usage
	}
}

func printHelp(command string, out io.Writer) {
	fmt.Fprint(out, usageFor(command))
	switch command {
	case "run":
		fmt.Fprint(out, "\npositional arguments:\n  chain\n\noptions:\n  -h, --help  show this help message and exit\n  --keep      skip cleanup; tear down later with `verilex cleanup`\n  --json      print the run record as JSON\n")
	case "cleanup":
		fmt.Fprint(out, "\npositional arguments:\n  run\n\noptions:\n  -h, --help  show this help message and exit\n")
	case "words", "runs":
		fmt.Fprint(out, "\noptions:\n  -h, --help  show this help message and exit\n")
	default:
		fmt.Fprint(out, "\nverilex command line.\n\npositional arguments:\n  {run,words,runs,cleanup}\n    run                 run a chain of words, e.g. 'a | b X | c'\n    words               list the dictionary\n    runs                list this project's runs and any instance still alive\n    cleanup             tear down a kept run's instance\n\noptions:\n  -h, --help            show this help message and exit\n  --project PROJECT     product checkout (default: cwd)\n")
	}
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

func report(record runner.Record, out io.Writer) {
	fmt.Fprintf(out, "verilex run %s (%s)\n", record.Run, record.Project)
	frames := map[string]runner.Frame{}
	for _, f := range record.Frame {
		frames[f.Step] = f
	}
	status := func(f runner.Frame) string {
		if f.Exit != nil && *f.Exit == 0 {
			return "ok"
		}
		return "refused"
	}
	for _, step := range []string{"launch", "doctor"} {
		if f, ok := frames[step]; ok {
			fmt.Fprintf(out, "  [%s] %s\n", status(f), step)
		}
	}
	for _, word := range record.Words {
		label := strings.Join(append([]string{word.Word}, word.Args...), " ")
		seconds := fmt.Sprint(word.Seconds)
		if !strings.Contains(seconds, ".") {
			seconds += ".0"
		}
		fmt.Fprintf(out, "  [%s] %s  (%ss)\n", word.Verdict, label, seconds)
		if runner.Truthy(word.Observation) {
			fmt.Fprintf(out, "      observation: %v\n", word.Observation)
		}
		if runner.Truthy(word.Reason) {
			fmt.Fprintf(out, "      reason: %v\n", word.Reason)
		}
		if word.Verdict != "pass" {
			fmt.Fprintf(out, "      fall back to the verify skill: %s\n", strings.Join(word.Implements, ", "))
		}
	}
	if f, ok := frames["doctor-after-failure"]; ok {
		fmt.Fprintf(out, "  [%s] doctor after failure\n", status(f))
	}
	verdict := "None"
	if record.Verdict != nil {
		verdict = *record.Verdict
	}
	fmt.Fprintf(out, "  cleanup: %s\nresult: %s", record.Cleanup, verdict)
	if record.Reason != nil && *record.Reason != "" {
		fmt.Fprintf(out, " - %s", *record.Reason)
	}
	fmt.Fprintf(out, "\nevidence: %s\n", record.Dir)
}

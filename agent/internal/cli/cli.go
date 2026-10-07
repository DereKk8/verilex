// Package cli is the verilex-agent command line.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/DereKk8/verilex/agent/internal/proxy"
	"github.com/DereKk8/verilex/agent/internal/run"
	"github.com/DereKk8/verilex/agent/internal/sandbox"
)

const usage = `usage: verilex-agent [--text] [--project DIR] [--verilex PATH] (--ticket FILE | --intent TEXT | --diff REV) [--claim NAME] [--harness NAME] [--model NAME] [--effort LEVEL] [--profile NAME] [--ledger DIR] [--home DIR] [--skill FILE] [--brain PATH] [--harnesses FILE] [--suggest] [--allow-harness] [--keep-work]

verilex-agent launches the brain named by the run spec, hands it the verilex skill and the intent, and prints verilex's own JSON verdict. The brain's message is ignored.
A green that does not cover the named claims and the diff, or no verilex run at all, is inconclusive (exit 2).
`

// Main runs one launcher invocation.
func Main(argv []string, stdout, stderr io.Writer) int {
	if len(argv) > 0 && argv[0] == "relay" {
		return relay(argv[1:], stdout, stderr)
	}
	if len(argv) > 0 && argv[0] == "sandbox-init" {
		return sandbox.Init(argv[1:], stderr)
	}
	opts, help, err := parse(argv)
	if help {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if err != nil {
		fmt.Fprint(stderr, usage)
		fmt.Fprintf(stderr, "verilex-agent: error: %v\n", err)
		return 2
	}
	result, err := run.Run(opts)
	if result.Work != "" {
		defer fmt.Fprintf(stderr, "verilex-agent: run files kept in %s\n", result.Work)
	}
	if inconclusive := (*run.Inconclusive)(nil); errors.As(err, &inconclusive) {
		fmt.Fprintf(stderr, "verilex-agent: inconclusive: %v\n", err)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "verilex-agent: refused: %v\n", err)
		return 2
	}
	stdout.Write(result.Stdout)
	return result.Exit
}

func relay(argv []string, stdout, stderr io.Writer) int {
	socket := ""
	args := argv
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "--socket":
			if len(args) < 2 {
				fmt.Fprintln(stderr, "verilex-agent: relay: --socket needs a path")
				return 2
			}
			socket = args[1]
			args = args[2:]
		case "--":
			args = args[1:]
			goto done
		default:
			if strings.HasPrefix(args[0], "--socket=") {
				socket = strings.TrimPrefix(args[0], "--socket=")
				args = args[1:]
				continue
			}
			fmt.Fprintf(stderr, "verilex-agent: relay: unknown flag %s\n", args[0])
			return 2
		}
	}
done:
	if socket == "" {
		fmt.Fprintln(stderr, "verilex-agent: relay: --socket is required")
		return 2
	}
	return proxy.Relay(socket, args, os.Stdin, stdout, stderr)
}

func parse(argv []string) (run.Options, bool, error) {
	opts := run.Options{JSON: true}
	for len(argv) > 0 {
		arg := argv[0]
		if arg == "--" {
			return opts, false, fmt.Errorf("unexpected %s", arg)
		}
		if !strings.HasPrefix(arg, "-") {
			return opts, false, fmt.Errorf("unexpected %s", arg)
		}
		argv = argv[1:]
		name, value, cut := strings.Cut(arg, "=")
		take := func() (string, error) {
			if cut {
				return value, nil
			}
			if len(argv) == 0 || strings.HasPrefix(argv[0], "-") {
				return "", fmt.Errorf("%s needs a value", name)
			}
			got := argv[0]
			argv = argv[1:]
			return got, nil
		}
		switch name {
		case "-h", "--help":
			return opts, true, nil
		case "--text":
			opts.JSON = false
		case "--json":
			opts.JSON = true
		case "--suggest":
			opts.Suggest = true
		case "--allow-harness":
			opts.AllowHarness = true
		case "--keep-work":
			opts.KeepWork = true
		case "--project", "--verilex", "--ticket", "--intent", "--diff", "--harness", "--model", "--effort", "--profile", "--ledger", "--home", "--skill", "--brain", "--harnesses":
			got, err := take()
			if err != nil {
				return opts, false, err
			}
			switch name {
			case "--project":
				opts.Project = got
			case "--verilex":
				opts.Verilex = got
			case "--ticket":
				opts.Ticket = got
			case "--intent":
				opts.Intent = got
			case "--diff":
				opts.Diff = got
			case "--harness":
				opts.Harness = got
			case "--model":
				opts.Model = got
			case "--effort":
				opts.Effort = got
			case "--profile":
				opts.Profile = got
			case "--ledger":
				opts.Ledger = got
			case "--home":
				opts.Home = got
			case "--skill":
				opts.Skill = got
			case "--brain":
				opts.Brain = got
			case "--harnesses":
				opts.Harnesses = got
			}
		case "--claim":
			got, err := take()
			if err != nil {
				return opts, false, err
			}
			opts.Claims = append(opts.Claims, got)
		default:
			return opts, false, fmt.Errorf("unknown flag %s", name)
		}
	}
	if opts.Ticket == "" && opts.Intent == "" && opts.Diff == "" {
		return opts, false, fmt.Errorf("a run needs --ticket, or --intent or --diff")
	}
	if opts.Ticket != "" && (opts.Intent != "" || opts.Diff != "" || opts.Harness != "" || opts.Model != "" || opts.Effort != "" || opts.Profile != "") {
		return opts, false, fmt.Errorf("--ticket already carries intent, diff and the brain; do not pass those flags as well")
	}
	return opts, false, nil
}

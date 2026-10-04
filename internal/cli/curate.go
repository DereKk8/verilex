package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/DereKk8/verilex/internal/curation"
	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/lifecycle"
)

func scaffold(root string, args options, out io.Writer, refuse func(error) int) int {
	s, err := curation.New(root, args.operand, args.implements)
	if err != nil {
		return refuse(err)
	}
	fmt.Fprintf(out, "new: provisional word %s implements %s\n", s.Word, strings.Join(args.implements, ", "))
	if s.Frame {
		fmt.Fprintf(out, "  created %s with config and frame stubs (%s); every run is inconclusive until they are written\n", filepath.Join(s.Project.Root, ".verilex"), strings.Join(curation.ScaffoldSteps, ", "))
	}
	fmt.Fprintf(out, "  write %s and %s\n", filepath.Join(".verilex", "words", s.Word, "word.md"), filepath.Join(".verilex", "words", s.Word, "run"))
	return 0
}

func curate(project dictionary.Project, args options, out io.Writer, refuse func(error) int) int {
	switch args.command {
	case "propose":
		packet, path, err := curation.Propose(project, args.operand)
		if err != nil {
			return refuse(err)
		}
		fmt.Fprintf(out, "proposed %s: packet %s (%d runs)\n  packet: %s\n  hand it to a curator, then `verilex admit %s --verdict <file>`\n", packet.Word, packet.ID, len(packet.Uses), path, packet.Word)
	case "admit":
		verdict, admission, err := curation.Admit(project, args.operand, args.verdict)
		if err != nil {
			return refuse(err)
		}
		if admission != nil {
			fmt.Fprintf(out, "admitted %s (curator %s, %d runs)\n", admission.Word, admission.Curator, len(admission.Runs))
			return 0
		}
		words, err := dictionary.LoadWords(project)
		if err != nil {
			return refuse(err)
		}
		for _, word := range words {
			if word.Name == verdict.Word {
				status, err := lifecycle.StatusOf(project, word)
				if err != nil {
					return refuse(err)
				}
				fmt.Fprintf(out, "rejected %s (curator %s): stays %s\n", verdict.Word, verdict.Curator, status.State)
			}
		}
	case "gap":
		path, err := curation.Gap(project, args.operand)
		if err != nil {
			return refuse(err)
		}
		fmt.Fprintf(out, "gap recorded for the verify skill's owner: %s\n", path)
	case "check":
		words, err := dictionary.LoadWords(project)
		if err != nil {
			return refuse(err)
		}
		admitted, drifted := 0, []lifecycle.Status{}
		for _, word := range words {
			status, err := lifecycle.StatusOf(project, word)
			if err != nil {
				return refuse(err)
			}
			if status.State != lifecycle.Provisional {
				admitted++
			}
			if status.State == lifecycle.DriftSuspect {
				drifted = append(drifted, status)
			}
		}
		if len(drifted) == 0 {
			fmt.Fprintf(out, "check: no drift (%d admitted)\n", admitted)
			return 0
		}
		fmt.Fprintf(out, "check: %d of %d admitted drift-suspect; they always run\n", len(drifted), admitted)
		for _, status := range drifted {
			fmt.Fprintf(out, "  %s: %s\n", status.Word, strings.Join(status.Drift, "; "))
		}
	}
	return 0
}

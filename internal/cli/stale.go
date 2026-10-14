package cli

import (
	"fmt"
	"io"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/index"
)

func stale(project dictionary.Project, out io.Writer, refuse func(error) int) int {
	ix, err := index.Build(project)
	if err != nil {
		return refuse(err)
	}
	code := 0
	for _, row := range ix.Stale(project) {
		status := "unproven"
		if code == 0 {
			code = 1
		}
		if row.Inconclusive {
			status, code = "inconclusive", 2
		}
		if _, err := fmt.Fprintf(out, "%s  %s: %s\n", status, row.Claim, row.Reason); err != nil {
			return refuse(err)
		}
	}
	return code
}

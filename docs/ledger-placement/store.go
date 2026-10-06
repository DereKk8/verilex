//go:build ignore

// The directory ledger under the same workload as service.go: n concurrent clients that each
// record the three passes of one chain run (store-open's slot shared by all) and then read the
// three steps back. Run with `go run store.go -n 32 -dir DIR` from this directory.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/DereKk8/verilex/internal/ledger"
	"github.com/DereKk8/verilex/internal/stamp"
)

func main() {
	n := flag.Int("n", 16, "concurrent clients")
	dir := flag.String("dir", "", "an empty directory for the ledger and the evidence")
	flag.Parse()
	steps := func(i int) []stamp.Stamp {
		result := []stamp.Stamp{}
		for _, name := range []string{"store-open", fmt.Sprintf("item-stored-%d", i), fmt.Sprintf("item-listed-%d", i)} {
			slot := fmt.Sprintf("%064x", len(name)*1000+i*(len(name)%7))
			if name == "store-open" {
				slot = fmt.Sprintf("%064x", 1)
			}
			result = append(result, stamp.Stamp{Slot: slot, Digest: fmt.Sprintf("%064x", 2), Components: map[string]string{"word": name}})
		}
		return result
	}
	evidence := filepath.Join(*dir, "evidence-src")
	os.MkdirAll(evidence, 0700)
	os.WriteFile(filepath.Join(evidence, "stdout"), []byte(`{"verdict": "pass", "observation": "store.json lists apple"}`+"\n"), 0600)
	os.WriteFile(filepath.Join(evidence, "exit"), []byte("0\n"), 0600)
	store := ledger.At(filepath.Join(*dir, "ledger"))
	var mu sync.Mutex
	writes, reads := []time.Duration{}, []time.Duration{}
	var wg sync.WaitGroup
	start := time.Now()
	for i := range *n {
		wg.Go(func() {
			run := fmt.Sprintf("1791290000-%012x", i)
			for _, s := range steps(i) {
				t := time.Now()
				if err := store.Record(map[string]ledger.Entry{s.Slot: {Label: s.Components["word"], Verdict: "green", Stamp: s.Digest, Components: s.Components, Run: run, Owner: run, Evidence: evidence, Observation: "store.json lists apple"}}); err != nil {
					fmt.Fprintln(os.Stderr, err)
				}
				mu.Lock()
				writes = append(writes, time.Since(t))
				mu.Unlock()
			}
			for _, s := range steps(i) {
				t := time.Now()
				if _, why := store.Reuse([]string{s.Components["word"]}, []string{""}, []stamp.Stamp{s}); why != "" {
					fmt.Fprintln(os.Stderr, why)
				}
				mu.Lock()
				reads = append(reads, time.Since(t))
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	total := time.Since(start)
	files, _ := filepath.Glob(filepath.Join(*dir, "ledger", "passes", "*", "*.json"))
	pct := func(d []time.Duration, p float64) string {
		slices.Sort(d)
		return fmt.Sprintf("%.2fms", float64(d[int(float64(len(d)-1)*p)].Microseconds())/1000)
	}
	fmt.Printf("directory n=%d: recorded %d of %d passes (lost %d); write p50 %s p95 %s; read p50 %s p95 %s; batch %.0fms\n",
		*n, len(files), 3**n, 3**n-len(files), pct(writes, 0.5), pct(writes, 0.95), pct(reads, 0.5), pct(reads, 0.95), float64(total.Microseconds())/1000)
}

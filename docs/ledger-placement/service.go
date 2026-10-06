//go:build ignore

// A minimal ledger service, built only to measure the "small service" placement: it keeps passes
// in memory behind one mutex and serves them over HTTP. Run with `go run service.go -n 32`: it
// starts the service on loopback, drives n concurrent clients that each record and read back
// passes, and prints latency percentiles and how many passes were lost.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"
)

type store struct {
	mu     sync.Mutex
	passes map[string][][]byte
}

func (s *store) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	slot := r.URL.Path[len("/passes/"):]
	switch r.Method {
	case http.MethodPost:
		body, err := io.ReadAll(r.Body)
		if err != nil || !json.Valid(body) {
			http.Error(w, "bad pass", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.passes[slot] = append(s.passes[slot], body)
		s.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	case http.MethodGet:
		s.mu.Lock()
		list := slices.Clone(s.passes[slot])
		s.mu.Unlock()
		w.Write([]byte("["))
		for i, p := range list {
			if i > 0 {
				w.Write([]byte(","))
			}
			w.Write(p)
		}
		w.Write([]byte("]"))
	}
}

func main() {
	n := flag.Int("n", 16, "concurrent clients")
	passFile := flag.String("pass", "", "a pass file to use as the payload")
	flag.Parse()
	payload, err := os.ReadFile(*passFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	s := &store{passes: map[string][][]byte{}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	go http.Serve(listener, s)
	base := "http://" + listener.Addr().String() + "/passes/"
	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: *n}}
	var mu sync.Mutex
	writes, reads := []time.Duration{}, []time.Duration{}
	var wg sync.WaitGroup
	start := time.Now()
	for i := range *n {
		wg.Go(func() {
			// Each client records the three passes of one chain run, store-open's slot shared by all.
			for _, slot := range []string{"store-open", fmt.Sprintf("item-stored-%d", i), fmt.Sprintf("item-listed-%d", i)} {
				t := time.Now()
				resp, err := client.Post(base+slot, "application/json", bytes.NewReader(payload))
				if err == nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
				mu.Lock()
				writes = append(writes, time.Since(t))
				mu.Unlock()
			}
			for _, slot := range []string{"store-open", fmt.Sprintf("item-stored-%d", i), fmt.Sprintf("item-listed-%d", i)} {
				t := time.Now()
				resp, err := client.Get(base + slot)
				if err == nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
				mu.Lock()
				reads = append(reads, time.Since(t))
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	total := time.Since(start)
	recorded := 0
	for _, list := range s.passes {
		recorded += len(list)
	}
	pct := func(d []time.Duration, p float64) string {
		slices.Sort(d)
		return fmt.Sprintf("%.2fms", float64(d[int(float64(len(d)-1)*p)].Microseconds())/1000)
	}
	fmt.Printf("service n=%d: recorded %d of %d passes (lost %d); write p50 %s p95 %s; read p50 %s p95 %s; batch %.0fms\n",
		*n, recorded, 3**n, 3**n-recorded, pct(writes, 0.5), pct(writes, 0.95), pct(reads, 0.5), pct(reads, 0.95), float64(total.Microseconds())/1000)
}

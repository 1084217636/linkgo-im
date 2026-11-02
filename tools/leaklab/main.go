// leaklab deliberately keeps goroutines and their buffers alive in an isolated
// process. It is a diagnostic exercise, not part of the Gateway binary.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sync"
	"time"
)

type snapshot struct {
	Goroutines int    `json:"goroutines"`
	HeapAlloc  uint64 `json:"heap_alloc_bytes"`
}

func sample() snapshot {
	runtime.GC()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return snapshot{Goroutines: runtime.NumGoroutine(), HeapAlloc: mem.HeapAlloc}
}

// startWorkers mirrors a common bug: workers wait for a shutdown signal that
// the owner forgets to send. KeepAlive makes the captured buffers visible in
// the heap profile while the workers are blocked.
func startWorkers(count int, shutdown bool) func() {
	done := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		buffer := make([]byte, 256<<10)
		buffer[0] = byte(i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-done
			runtime.KeepAlive(buffer)
		}()
	}
	var once sync.Once
	stop := func() {
		once.Do(func() { close(done) })
		wg.Wait()
	}
	if shutdown {
		stop()
	}
	return stop
}

func writeProfiles(dir, phase string) error {
	heapPath := filepath.Join(dir, "heap-"+phase+".pb.gz")
	heapFile, err := os.Create(heapPath)
	if err != nil {
		return err
	}
	if err := pprof.WriteHeapProfile(heapFile); err != nil {
		_ = heapFile.Close()
		return err
	}
	if err := heapFile.Close(); err != nil {
		return err
	}
	stackPath := filepath.Join(dir, "goroutines-"+phase+".txt")
	stackFile, err := os.Create(stackPath)
	if err != nil {
		return err
	}
	if err := pprof.Lookup("goroutine").WriteTo(stackFile, 2); err != nil {
		_ = stackFile.Close()
		return err
	}
	return stackFile.Close()
}

func main() {
	mode := flag.String("mode", "leak", "leak or fixed")
	count := flag.Int("count", 80, "number of demo workers")
	dir := flag.String("out", "/tmp/linkgo-leaklab", "directory for profiles")
	flag.Parse()
	if (*mode != "leak" && *mode != "fixed") || *count < 1 || *count > 1000 {
		fmt.Fprintln(os.Stderr, "mode must be leak or fixed; count must be 1..1000")
		os.Exit(2)
	}
	if err := os.MkdirAll(*dir, 0o700); err != nil {
		panic(err)
	}
	before := sample()
	if err := writeProfiles(*dir, "before"); err != nil {
		panic(err)
	}
	stop := startWorkers(*count, *mode == "fixed")
	defer stop() // Only after the leak snapshot, so this exercise exits cleanly.
	time.Sleep(100 * time.Millisecond)
	after := sample()
	if err := writeProfiles(*dir, "after"); err != nil {
		panic(err)
	}
	result := map[string]any{"mode": *mode, "count": *count, "before": before, "after": after, "profile_dir": *dir}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		panic(err)
	}
}

package analyze

import (
	"runtime"
	"sync"
)

// All analyzes every file concurrently. Each worker owns one AST engine
// (a wasm instance) for its whole batch; workers are capped both by CPU
// count and by batch size so small runs don't pay several engine startups.
// Unreadable or unsupported files are skipped, mirroring File's callers.
func (p *Project) All(rels []string) map[string]*Result {
	workers := runtime.NumCPU()
	if n := (len(rels) + 63) / 64; n < workers {
		workers = n
	}
	if workers < 1 {
		workers = 1
	}

	jobs := make(chan string)
	results := make(map[string]*Result, len(rels))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			eng := getEngine(p.engineOff)
			defer putEngine(eng)
			for rel := range jobs {
				res, err := p.fileWith(rel, eng)
				if err != nil {
					continue
				}
				mu.Lock()
				results[rel] = res
				mu.Unlock()
			}
		}()
	}
	for _, rel := range rels {
		jobs <- rel
	}
	close(jobs)
	wg.Wait()
	return results
}

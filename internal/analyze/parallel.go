package analyze

import (
	"runtime"
	"sync"
)

// All analyzes every file concurrently, one worker per CPU core.
// Unreadable or unsupported files are skipped, mirroring File's callers.
func (p *Project) All(rels []string) map[string]*Result {
	results := make(map[string]*Result, len(rels))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for _, rel := range rels {
		wg.Add(1)
		sem <- struct{}{}
		go func(rel string) {
			defer wg.Done()
			defer func() { <-sem }()
			res, err := p.File(rel)
			if err != nil {
				return
			}
			mu.Lock()
			results[rel] = res
			mu.Unlock()
		}(rel)
	}
	wg.Wait()
	return results
}

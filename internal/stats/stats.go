// Package stats samples CPU and memory use for a process and everything it
// has spawned.
package stats

import (
	"sort"

	"github.com/shirou/gopsutil/v4/process"
)

// Sample is the resource use of one process tree at a point in time.
type Sample struct {
	// CPU is the summed CPU use of the tree since the previous sample, where
	// 100 means one fully busy core (so it can exceed 100 on multi-core
	// machines, as in top).
	CPU float64
	// RSS is the summed resident memory, in bytes. Pages shared between
	// processes in the tree are counted once per process.
	RSS uint64
	// Procs is how many processes are in the tree, root included.
	Procs int
}

// Collector samples process trees. It remembers per-process CPU times between
// calls, so it must be reused across samples and is not safe for concurrent
// use.
type Collector struct {
	known map[int32]*process.Process
}

// NewCollector returns an empty collector.
func NewCollector() *Collector {
	return &Collector{known: map[int32]*process.Process{}}
}

// Sample measures the tree rooted at each pid in roots. The process table is
// read once and shared by all roots. A root that no longer exists is left out
// of the result.
//
// A tree is the root plus every descendant found by following parent pids.
// A grandchild whose parent has already exited has been re-parented to init
// and is no longer part of the tree.
func (c *Collector) Sample(roots []int) map[int]Sample {
	out := make(map[int]Sample, len(roots))
	if len(roots) == 0 {
		return out
	}
	children := childrenByParent()

	seen := map[int32]bool{}
	for _, root := range roots {
		tree := descendants(children, int32(root))
		var s Sample
		for _, pid := range tree {
			seen[pid] = true
			p := c.lookup(pid)
			if p == nil {
				continue
			}
			mem, err := p.MemoryInfo()
			if err != nil {
				continue // gone, or not ours to read
			}
			s.Procs++
			s.RSS += mem.RSS
			if cpu, err := p.Percent(0); err == nil {
				s.CPU += cpu
			}
		}
		if s.Procs > 0 {
			out[root] = s
		}
	}
	// Forget processes that left every tree so the cache cannot grow.
	for pid := range c.known {
		if !seen[pid] {
			delete(c.known, pid)
		}
	}
	return out
}

func (c *Collector) lookup(pid int32) *process.Process {
	if p, ok := c.known[pid]; ok {
		return p
	}
	p, err := process.NewProcess(pid)
	if err != nil {
		return nil
	}
	c.known[pid] = p
	return p
}

// childrenByParent reads the process table once and maps each parent pid to
// its children.
func childrenByParent() map[int32][]int32 {
	pids, err := process.Pids()
	if err != nil {
		return nil
	}
	out := make(map[int32][]int32, len(pids))
	for _, pid := range pids {
		p, err := process.NewProcess(pid)
		if err != nil {
			continue
		}
		ppid, err := p.Ppid()
		if err != nil {
			continue
		}
		out[ppid] = append(out[ppid], pid)
	}
	return out
}

// descendants returns root followed by all of its descendants. Cycles (which
// a recycled pid can briefly produce) are tolerated.
func descendants(children map[int32][]int32, root int32) []int32 {
	tree := []int32{root}
	seen := map[int32]bool{root: true}
	for i := 0; i < len(tree); i++ {
		kids := append([]int32(nil), children[tree[i]]...)
		sort.Slice(kids, func(a, b int) bool { return kids[a] < kids[b] })
		for _, k := range kids {
			if !seen[k] {
				seen[k] = true
				tree = append(tree, k)
			}
		}
	}
	return tree
}

// Keep track of various build statistics.

package main

import (
	"sync"
	"time"
)

// A Statistics structure encapsulates various run-time Makefile statistics.
type Statistics struct {
	maxConc int64 // Maximum concurrency the Makefile can exploit

	totalProcs int64                   // Total number of processes spawned by GNU Make
	pidToTime  map[pid_t]time.Duration // Wall-clock time a process consumed
	children   map[pid_t][]pid_t       // Association of a parent with all of its children
	seqTime    time.Duration           // Total time if the Makefile ran sequentially
	parTime    time.Duration           // Total time if the Makefile ran on an infinite number of processors
	lock       sync.Mutex              // Protect the entire structure
}

// NewStatistics returns an initialized Statistics structure.
func NewStatistics() *Statistics {
	return &Statistics{
		pidToTime: make(map[pid_t]time.Duration),
		children:  make(map[pid_t][]pid_t),
	}
}

// ObserveConcurrency keeps track of the current concurrency level.
func (st *Statistics) ObserveConcurrency(conc int64) {
	st.lock.Lock()
	defer st.lock.Unlock()
	if conc > st.maxConc {
		st.maxConc = conc
	}
}

// ObserveSpawn associates a fake parent PID with a fake child PID.
func (st *Statistics) ObserveSpawn(parent, child pid_t) {
	_, ok := st.children[parent]
	if !ok {
		st.children[parent] = make([]pid_t, 0, 4)
	}
	st.children[parent] = append(st.children[parent], child)
}

// ObserveExecution keeps track of a process that finished executing.
func (st *Statistics) ObserveExecution(pid, mkPid pid_t, elapsed time.Duration) {
	st.lock.Lock()
	defer st.lock.Unlock()
	st.totalProcs++
	st.pidToTime[pid] = elapsed
	if mkPid > 0 {
		st.ObserveSpawn(mkPid, pid)
	}
}

// GetMaxConcurrency returns the maximum concurrency observed.
func (st *Statistics) GetMaxConcurrency() int64 {
	st.lock.Lock()
	defer st.lock.Unlock()
	return st.maxConc
}

// GetTotalProcesses returns the total number of processes run.
func (st *Statistics) GetTotalProcesses() int64 {
	st.lock.Lock()
	defer st.lock.Unlock()
	return st.totalProcs
}

// GetSequentialTime returns the time the Makefile would have run if run
// sequentially (and without stress-make overhead).
func (st *Statistics) GetSequentialTime() time.Duration {
	st.lock.Lock()
	defer st.lock.Unlock()
	return st.seqTime
}

// GetParallelTime returns the time the Makefile would have run if run
// on an infinite number of processors.
func (st *Statistics) GetParallelTime() time.Duration {
	st.lock.Lock()
	defer st.lock.Unlock()
	return st.parTime
}

// Finalize prepares a Statistics for output.  It should be called before any
// of the Get* functions.
func (st *Statistics) Finalize() {
	st.lock.Lock()
	defer st.lock.Unlock()

	// GNU Make processes have no time recorded.  Propagate up
	// their children's time.
	var propagateTimeUp func(pid_t) time.Duration
	propagateTimeUp = func(pid pid_t) time.Duration {
		pidTime := st.pidToTime[pid]
		kidPids, ok := st.children[pid]
		if !ok {
			// Leaf (no children)
			return pidTime
		}
		for _, kPid := range kidPids {
			pidTime += propagateTimeUp(kPid)
		}
		if st.pidToTime[pid] == 0 {
			st.pidToTime[pid] = pidTime
		}
		return st.pidToTime[pid]
	}
	propagateTimeUp(0)

	// pidToTime now represents inclusive time (PID + children).  Make it
	// represent exclusive time (PID by itself).
	var incltoExcl func(pid_t)
	incltoExcl = func(pid pid_t) {
		// Subtract each child's time from its parent's time then
		// perform the same operation recursively on the child.
		kidPids, ok := st.children[pid]
		if !ok {
			// Leaf (no children)
			return
		}
		parentTime := st.pidToTime[pid]
		for _, kPid := range kidPids {
			parentTime -= st.pidToTime[kPid]
			incltoExcl(kPid)
		}
		st.pidToTime[pid] = parentTime
	}
	incltoExcl(0)
	st.pidToTime[0] = 0

	// Add up all exclusive times to produce a total sequential time.
	for _, dur := range st.pidToTime {
		st.seqTime += dur
	}

	// Compute the critical-path time as the longest-time path from the
	// root of the tree (PID 0) to any leaf.
	var findCritPath func(pid_t, time.Duration)
	findCritPath = func(pid pid_t, timeExclPid time.Duration) {
		timeInclPid := timeExclPid + st.pidToTime[pid]
		kidPids, ok := st.children[pid]
		if !ok {
			// PID is a leaf.  See if we broke the time record.
			if timeInclPid > st.parTime {
				st.parTime = timeInclPid
			}
			return
		}
		for _, kPid := range kidPids {
			// Recursively follow each subpath.
			findCritPath(kPid, timeInclPid)
		}
	}
	findCritPath(0, 0)
}

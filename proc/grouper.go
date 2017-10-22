package proc

import (
	"time"

	common "github.com/ncabatoff/process-exporter"
)

type (
	// Grouper is the top-level interface to the process metrics.  All tracked
	// procs sharing the same group name are aggregated.
	Grouper struct {
		// groupAccum records the historical accumulation of a group so that
		// we can avoid ever decreasing the counts we return.
		groupAccum map[string]Counts
		tracker    *Tracker
	}

	// GroupByName maps group name to group metrics.
	GroupByName map[string]Group

	// Group describes the metrics of a single group.
	Group struct {
		Counts
		Procs int
		Memory
		OldestStartTime time.Time
		OpenFDs         uint64
		WorstFDratio    float64
		NumThreads      uint64
	}
)

// NewGrouper creates a grouper.
func NewGrouper(trackChildren bool, namer common.MatchNamer) *Grouper {
	g := Grouper{
		groupAccum: make(map[string]Counts),
		tracker:    NewTracker(namer, trackChildren),
	}
	return &g
}

func groupadd(grp Group, ts Update) Group {
	var zeroTime time.Time

	grp.Procs++
	grp.Memory.ResidentBytes += ts.Memory.ResidentBytes
	grp.Memory.VirtualBytes += ts.Memory.VirtualBytes
	if ts.Filedesc.Open != -1 {
		grp.OpenFDs += uint64(ts.Filedesc.Open)
	}
	openratio := float64(ts.Filedesc.Open) / float64(ts.Filedesc.Limit)
	if grp.WorstFDratio < openratio {
		grp.WorstFDratio = openratio
	}
	grp.NumThreads += ts.NumThreads
	grp.Counts.Add(ts.Latest)
	if grp.OldestStartTime == zeroTime || ts.Start.Before(grp.OldestStartTime) {
		grp.OldestStartTime = ts.Start
	}

	return grp
}

// Update takes a snapshot of currently running processes and passes them
// to the tracker.  Whatever updates the tracker returns are aggregated by
// groupname, augmented by accumulated counts from the past, and returned.
func (g *Grouper) Update(iter Iter) (CollectErrors, GroupByName, error) {
	cerrs, tracked, err := g.tracker.Update(iter)
	if err != nil {
		return cerrs, nil, err
	}
	groups := make(GroupByName)

	for _, update := range tracked {
		groups[update.GroupName] = groupadd(groups[update.GroupName], update)
	}

	// Add any accumulated counts to what was just observed,
	// and update the accumulators.
	for gname, group := range groups {
		if oldcounts, ok := g.groupAccum[gname]; ok {
			group.Counts.Add(oldcounts)
		}
		g.groupAccum[gname] = group.Counts
		groups[gname] = group
	}

	// Now add any groups that were observed in the past but aren't running now.
	for gname, gcounts := range g.groupAccum {
		if _, ok := groups[gname]; !ok {
			groups[gname] = Group{Counts: gcounts}
		}
	}

	return cerrs, groups, nil
}

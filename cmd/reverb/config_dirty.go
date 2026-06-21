package main

import "sync/atomic"

// atomicDirty is the restart-to-apply flag: any adapter/settings mutation flips it,
// and GET /config/pending-restart reports it so the UI shows a "Restart to apply"
// banner. M4a applies adapter changes on the next process start (no hot-reload).
type atomicDirty struct{ b atomic.Bool }

func (d *atomicDirty) Set()        { d.b.Store(true) }
func (d *atomicDirty) Dirty() bool { return d.b.Load() }

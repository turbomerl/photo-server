package convert

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
)

// Job is a pending conversion of a stored original.
type Job struct {
	Hash string
	Ext  string // original extension, e.g. ".heic"
	MIME string // image/heic, image/jpeg, ...
}

// Pool runs conversions on a bounded set of workers so a speech-time
// upload burst can't thrash the fanless Dell (PRD R5). Enqueue is
// non-blocking and never on the HTTP response path.
type Pool struct {
	conv    *Converter
	log     *slog.Logger
	workers int
	jobs    chan Job
	wg      sync.WaitGroup
	inFlt   sync.Map // hash -> struct{}: de-dupe queued/running work
	stopped atomic.Bool
}

// NewPool builds a pool with `workers` workers and a queue depth of
// `queue` (clamped to at least `workers`).
func NewPool(conv *Converter, workers, queue int, log *slog.Logger) *Pool {
	if workers < 1 {
		workers = 1
	}
	if queue < workers {
		queue = workers
	}
	return &Pool{conv: conv, log: log, workers: workers, jobs: make(chan Job, queue)}
}

// Start launches the workers. They exit when ctx is cancelled
// (shutdown); Stop then waits for in-flight work to finish.
func (p *Pool) Start(ctx context.Context) {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(ctx)
	}
}

func (p *Pool) worker(ctx context.Context) {
	defer p.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-p.jobs:
			// Every photo needs a grid thumbnail; only HEIC/HEIF
			// additionally needs a browser-viewable gallery JPEG.
			if err := p.conv.Thumbnail(ctx, j.Hash, j.Ext); err != nil {
				if ctx.Err() == nil {
					p.log.Error("thumbnail conversion failed", "hash", j.Hash, "err", err)
				}
			}
			if j.MIME == "image/heic" || j.MIME == "image/heif" {
				if err := p.conv.GalleryJPEG(ctx, j.Hash, j.Ext); err != nil {
					if ctx.Err() == nil {
						p.log.Error("gallery conversion failed", "hash", j.Hash, "err", err)
					}
				}
			}
			p.inFlt.Delete(j.Hash)
			p.log.Debug("renditions ready", "hash", j.Hash)
		}
	}
}

// Enqueue submits a conversion. Non-blocking and de-duplicated by
// hash: if the queue is full it logs and drops the job — the startup
// backfill picks it up next run, so nothing is permanently lost.
func (p *Pool) Enqueue(hash, ext, mime string) {
	if p.stopped.Load() {
		return
	}
	if _, dup := p.inFlt.LoadOrStore(hash, struct{}{}); dup {
		return
	}
	select {
	case p.jobs <- Job{Hash: hash, Ext: ext, MIME: mime}:
	default:
		p.inFlt.Delete(hash)
		p.log.Warn("convert queue full; deferring to backfill", "hash", hash)
	}
}

// Stop marks the pool closed for new work and waits for in-flight
// conversions to finish (workers also exit on ctx cancel).
func (p *Pool) Stop() {
	p.stopped.Store(true)
	p.wg.Wait()
}

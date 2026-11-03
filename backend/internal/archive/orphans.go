package archive

import (
	"context"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// orphanMinAge keeps cleanup away from uploads in flight and from a catalog commit that is
	// about to happen (ADR 0008).
	orphanMinAge = 24 * time.Hour
	// orphanEvery is how often one process reconciles. A full listing is expensive on S3.
	orphanEvery = 24 * time.Hour
	orphanBatch = 500
)

// OrphanReport says what one reconciliation found.
type OrphanReport struct {
	Providers int
	Listed    int
	Deleted   int
	Failed    int
}

// rawDeleter is implemented by providers whose listing can show files that are not valid keys,
// such as SFTP ".part" leftovers of an interrupted upload.
type rawDeleter interface {
	DeleteRaw(ctx context.Context, listed string) error
}

// ReconcileOrphans deletes remote objects that no catalog row owns and that are older than a
// day: final objects of segments that are gone from the catalog, temporary objects of segments
// that can no longer promote them, and interrupted SFTP uploads. Files outside the key layout
// this package writes (v1/ and tmp/) are never touched, so a shared bucket keeps its other data.
func (a *Archiver) ReconcileOrphans(ctx context.Context, now time.Time) (OrphanReport, error) {
	var rep OrphanReport
	sps, err := a.Store.ListProviders(ctx)
	if err != nil {
		return rep, err
	}
	for _, sp := range sps {
		p, err := a.provider(ctx, sp.ID)
		if err != nil {
			a.Log.Warn("orphan cleanup: provider unavailable", "provider", sp.Name, "err", err)
			rep.Failed++
			continue
		}
		rep.Providers++
		if err := a.reconcileProvider(ctx, now, sp.ID, p, &rep); err != nil {
			if ctx.Err() != nil {
				return rep, ctx.Err()
			}
			a.Log.Warn("orphan cleanup: provider failed", "provider", sp.Name, "err", err)
			rep.Failed++
		}
	}
	return rep, nil
}

func (a *Archiver) reconcileProvider(ctx context.Context, now time.Time, providerID uuid.UUID, p Provider, rep *OrphanReport) error {
	var finals []string
	tmps := map[uuid.UUID]string{}
	var parts []string
	cutoff := now.Add(-orphanMinAge)
	// Collect first, delete after: some providers hold their connection during List.
	err := p.List(ctx, "", func(o ObjectInfo) error {
		rep.Listed++
		if o.ModTime.After(cutoff) {
			return nil
		}
		switch {
		case strings.HasPrefix(o.Key, "v1/") && strings.HasSuffix(o.Key, ".parquet") && ValidKey(o.Key) == nil:
			finals = append(finals, o.Key)
		case strings.HasPrefix(o.Key, "tmp/"):
			if id, ok := tmpID(o.Key); ok {
				tmps[id] = o.Key
			} else if isPartFile(o.Key) {
				parts = append(parts, o.Key)
			}
		case strings.HasPrefix(o.Key, "v1/") && isPartFile(o.Key):
			parts = append(parts, o.Key)
		}
		return nil
	})
	if err != nil {
		return err
	}

	del := func(key string, raw bool) {
		var err error
		if raw {
			rd, ok := p.(rawDeleter)
			if !ok {
				return
			}
			err = rd.DeleteRaw(ctx, key)
		} else {
			err = p.Delete(ctx, key)
		}
		if err != nil {
			a.Log.Warn("orphan cleanup: delete failed", "key", key, "err", err)
			rep.Failed++
			return
		}
		a.Log.Info("orphan cleanup: deleted", "key", key)
		rep.Deleted++
	}

	for start := 0; start < len(finals); start += orphanBatch {
		batch := finals[start:min(start+orphanBatch, len(finals))]
		owned, err := a.Store.ArchivedKeys(ctx, providerID, batch)
		if err != nil {
			return err
		}
		for _, k := range batch {
			if !owned[k] {
				del(k, false)
			}
		}
	}

	ids := make([]uuid.UUID, 0, len(tmps))
	for id := range tmps {
		ids = append(ids, id)
	}
	for start := 0; start < len(ids); start += orphanBatch {
		batch := ids[start:min(start+orphanBatch, len(ids))]
		pending, err := a.Store.PendingSegmentIDs(ctx, batch)
		if err != nil {
			return err
		}
		for _, id := range batch {
			if !pending[id] {
				del(tmps[id], false)
			}
		}
	}

	for _, k := range parts {
		del(k, true)
	}
	return nil
}

// tmpID parses "tmp/<segment id>.parquet".
func tmpID(key string) (uuid.UUID, bool) {
	name, ok := strings.CutPrefix(key, "tmp/")
	if !ok {
		return uuid.Nil, false
	}
	name, ok = strings.CutSuffix(name, ".parquet")
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(name)
	return id, err == nil && id.String() == name
}

// isPartFile matches the hidden ".<name>.part-<random>" files SFTP.Put writes before renaming.
func isPartFile(key string) bool {
	base := path.Base(key)
	return strings.HasPrefix(base, ".") && strings.Contains(base, ".parquet.part-") && !strings.Contains(key, "..")
}

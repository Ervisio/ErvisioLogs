package wal

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"
)

// BenchmarkAppend reports ack latency percentiles for 1 KiB records with 1 and 32 concurrent writers.
func BenchmarkAppend(b *testing.B) {
	for _, writers := range []int{1, 32} {
		b.Run(map[int]string{1: "writers=1", 32: "writers=32"}[writers], func(b *testing.B) {
			l, err := Open(Options{Dir: b.TempDir(), MaxBytes: 8 << 30})
			if err != nil {
				b.Fatal(err)
			}
			defer l.Close()
			rec := make([]byte, 1024)
			lat := make([]time.Duration, b.N)
			var wg sync.WaitGroup
			next := make(chan int, b.N)
			for i := 0; i < b.N; i++ {
				next <- i
			}
			close(next)
			b.ResetTimer()
			for w := 0; w < writers; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := range next {
						t := time.Now()
						if _, err := l.Append(context.Background(), rec); err != nil {
							b.Error(err)
							return
						}
						lat[i] = time.Since(t)
					}
				}()
			}
			wg.Wait()
			b.StopTimer()
			sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
			p := func(q float64) float64 { return float64(lat[int(q*float64(len(lat)-1))].Microseconds()) }
			b.ReportMetric(p(0.50), "p50_us")
			b.ReportMetric(p(0.95), "p95_us")
			b.ReportMetric(p(0.99), "p99_us")
			st := l.Stats()
			b.ReportMetric(float64(st.Appends)/float64(max(st.Syncs, 1)), "appends/fsync")
		})
	}
}

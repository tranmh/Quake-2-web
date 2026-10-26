package auth

import (
	"fmt"
	"testing"
	"time"
)

var authBenchSink interface{}

func BenchmarkTicketsIssueRedeem(b *testing.B) {
	for _, n := range []int{0, 1000, 10000} {
		b.Run(fmt.Sprintf("live=%d", n), func(b *testing.B) {
			t := NewTickets(time.Minute)
			for i := 0; i < n; i++ {
				if _, err := t.Issue(int64(i), "u", "g"); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tk, err := t.Issue(1, "u", "g")
				if err != nil {
					b.Fatal(err)
				}
				r, err := t.Redeem(tk.Token, "g")
				if err != nil {
					b.Fatal(err)
				}
				authBenchSink = r
			}
		})
	}
}

func BenchmarkLimiterAllow(b *testing.B) {
	for _, n := range []int{1, 1000, 100000} {
		b.Run(fmt.Sprintf("keys=%d", n), func(b *testing.B) {
			l := NewLimiter(1<<30, time.Hour)
			keys := make([]string, n)
			for i := range keys {
				keys[i] = fmt.Sprintf("10.0.%d.%d", i/256, i%256)
				l.Allow(keys[i])
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ok, _ := l.Allow(keys[i%n])
				authBenchSink = ok
			}
		})
	}
}

func BenchmarkVerifyPassword(b *testing.B) {
	for _, mem := range []uint32{1024, 8 * 1024, DefaultParams.Memory} {
		b.Run(fmt.Sprintf("memKiB=%d", mem), func(b *testing.B) {
			p := DefaultParams
			p.Memory = mem
			h, err := HashPassword("correct horse battery staple", p)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ok, err := VerifyPassword(h, "correct horse battery staple")
				if err != nil {
					b.Fatal(err)
				}
				authBenchSink = ok
			}
		})
	}
}

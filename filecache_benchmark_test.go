package filecache

import (
	"bytes"
	"os"
	"sync"
	"testing"
	"time"
)

func createTempFile(t *testing.T, size int) string {
	t.Helper()
	f, err := os.CreateTemp("", "filecache-bench-*")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data := bytes.Repeat([]byte("x"), size)
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

func setupCache(t *testing.T) *FileCache {
	cache := NewDefaultCache()
	cache.MaxItems = 1000
	cache.MaxSize = 1024 * 1024 // 1 MB
	cache.ExpireItem = 5
	cache.Every = 1
	if err := cache.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cache.Stop() })
	return cache
}

func BenchmarkCacheNow(b *testing.B) {
	cache := setupCache(&testing.T{})
	file := createTempFile(&testing.T{}, 4096)
	defer os.Remove(file)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := cache.CacheNow(file); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetItem(b *testing.B) {
	cache := setupCache(&testing.T{})
	file := createTempFile(&testing.T{}, 4096)
	defer os.Remove(file)
	if err := cache.CacheNow(file); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := cache.GetItem(file); !ok {
			b.Fatal("item missing from cache")
		}
	}
}

func BenchmarkConcurrentReadWrite(b *testing.B) {
	cache := setupCache(&testing.T{})
	file := createTempFile(&testing.T{}, 4096)
	defer os.Remove(file)

	b.ResetTimer()
	var wg sync.WaitGroup
	for i := 0; i < b.N; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = cache.CacheNow(file)
		}()
		go func() {
			defer wg.Done()
			cache.GetItem(file)
		}()
	}
	wg.Wait()
}

func BenchmarkVacuum(b *testing.B) {
	cache := setupCache(&testing.T{})
	file := createTempFile(&testing.T{}, 4096)
	defer os.Remove(file)

	for i := 0; i < 200; i++ {
		_ = cache.CacheNow(file)
	}

	cache.mutex.Lock()
	for _, itm := range cache.items {
		itm.Lastaccess = time.Now().Add(-10 * time.Second)
	}
	cache.mutex.Unlock()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.vacuumOnce()
	}
}

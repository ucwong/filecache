package filecache

import (
	"os"
	"testing"
	"time"
)

func TestCacheAndGetItem(t *testing.T) {
	cache := NewDefaultCache()
	cache.MaxItems = 10
	cache.Every = 0 // 不启动vacuum
	if err := cache.Start(); err != nil {
		t.Fatal(err)
	}
	defer cache.Stop()

	file := createTempFile(t, 32)
	defer os.Remove(file)

	if err := cache.CacheNow(file); err != nil {
		t.Fatalf("CacheNow failed: %v", err)
	}

	content, ok := cache.GetItem(file)
	if !ok {
		t.Fatalf("GetItem should return true")
	}
	if len(content) != 32 {
		t.Errorf("expected 32 bytes, got %d", len(content))
	}
}

func TestVacuumOnceExpiresItems(t *testing.T) {
	cache := NewDefaultCache()
	cache.ExpireItem = 1
	cache.Every = 0
	if err := cache.Start(); err != nil {
		t.Fatal(err)
	}
	defer cache.Stop()

	file := createTempFile(t, 16)
	defer os.Remove(file)
	_ = cache.CacheNow(file)

	cache.mutex.Lock()
	for _, itm := range cache.items {
		itm.Lastaccess = time.Now().Add(-5 * time.Second)
	}
	cache.mutex.Unlock()

	cache.vacuumOnce()

	if cache.InCache(file) {
		t.Errorf("expected file to be expired and removed from cache")
	}
}

func TestExpireOldest_1(t *testing.T) {
	cache := NewDefaultCache()
	cache.MaxItems = 1
	cache.Every = 0
	if err := cache.Start(); err != nil {
		t.Fatal(err)
	}
	defer cache.Stop()

	file1 := createTempFile(t, 8)
	file2 := createTempFile(t, 8)
	defer os.Remove(file1)
	defer os.Remove(file2)

	_ = cache.CacheNow(file1)
	_ = cache.CacheNow(file2)

	if cache.Size() > cache.MaxItems {
		t.Errorf("expected cache size <= MaxItems, got %d", cache.Size())
	}
	if cache.InCache(file1) && cache.InCache(file2) {
		t.Errorf("expected oldest file to be evicted")
	}
}

func TestRemoveItem(t *testing.T) {
	cache := NewDefaultCache()
	cache.MaxItems = 10
	if err := cache.Start(); err != nil {
		t.Fatal(err)
	}
	defer cache.Stop()

	file := createTempFile(t, 8)
	defer os.Remove(file)
	_ = cache.CacheNow(file)

	ok, err := cache.Remove(file)
	if err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	if !ok {
		t.Errorf("Remove should return ok=true")
	}
	if cache.InCache(file) {
		t.Errorf("file should no longer be in cache")
	}
}

func TestAccessResetsExpiration(t *testing.T) {
	cache := NewDefaultCache()
	cache.ExpireItem = 2
	cache.Every = 0
	if err := cache.Start(); err != nil {
		t.Fatal(err)
	}
	defer cache.Stop()

	file := createTempFile(t, 8)
	defer os.Remove(file)
	_ = cache.CacheNow(file)

	cache.mutex.Lock()
	for _, itm := range cache.items {
		itm.Lastaccess = time.Now().Add(-1 * time.Second)
	}
	cache.mutex.Unlock()

	cache.GetItem(file)

	cache.mutex.RLock()
	lastAccess := cache.items[file].Lastaccess
	cache.mutex.RUnlock()

	if time.Since(lastAccess) > time.Second {
		t.Errorf("expected last access to be refreshed")
	}
}

package filecache

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"
)

func getTimeExpiredCacheItem() *cacheItem {
	TwoHours, err := time.ParseDuration("-2h")
	if err != nil {
		panic(err.Error())
	}
	itm := new(cacheItem)
	itm.content = []byte("this cache item should be expired")
	itm.Lastaccess = time.Now().Add(TwoHours)
	return itm
}

func (cache *FileCache) _add_cache_item(name string, itm *cacheItem) {
	itm.name = name
	cache.items[name] = itm
}

func dumpModTime(name string) {
	fi, err := os.Stat(name)
	if err != nil {
		panic(err.Error())
	}

	fmt.Printf("[-] %s mod time: %v\n", name, fi.ModTime().Unix())
}

func writeTempFile(t *testing.T, contents string) string {
	tmpf, err := os.CreateTemp("", "fctest")
	if err != nil {
		fmt.Println("failed")
		fmt.Println("[!] couldn't create temporary file: ", err.Error())
		t.Fail()
	}
	name := tmpf.Name()
	tmpf.Close()
	err = os.WriteFile(name, []byte(contents), 0600)
	if err != nil {
		fmt.Println("failed")
		fmt.Println("[!] couldn't write temporary file: ", err.Error())
		os.Remove(name)
		t.Fail()
		name = ""
		return name
	}
	return name
}

func TestCacheStartStop(t *testing.T) {
	fmt.Printf("[+] testing cache start up and shutdown: ")
	cache := NewDefaultCache()
	if err := cache.Start(); err != nil {
		fmt.Println("failed")
		fmt.Println("[!] cache failed to start: ", err.Error())
		return
	}
	defer cache.Stop()
	time.Sleep(1 * time.Second)
	fmt.Println("ok")
}

func TestTimeExpiration(t *testing.T) {
	fmt.Printf("[+] ensure item expires after ExpireItem: ")
	cache := NewDefaultCache()
	if err := cache.Start(); err != nil {
		fmt.Println("failed")
		fmt.Println("[!] cache failed to start: ", err.Error())
		return
	}
	defer cache.Stop()
	name := "expired"
	itm := getTimeExpiredCacheItem()
	cache._add_cache_item(name, itm)
	if !cache.expired(name) {
		fmt.Println("failed")
		fmt.Println("[!] item should have expired!")
		t.Fail()
	} else {
		fmt.Println("ok")
	}
}

func TestTimeExpirationUpdate(t *testing.T) {
	fmt.Printf("[+] ensure accessing an item prevents it from expiring: ")
	cache := NewDefaultCache()
	cache.ExpireItem = 2
	cache.Every = 1
	if err := cache.Start(); err != nil {
		fmt.Println("failed")
		fmt.Println("[!] cache failed to start: ", err.Error())
		return
	}
	defer cache.Stop()
	testFile := "filecache.go"
	cache.CacheNow(testFile)
	if !cache.InCache(testFile) {
		fmt.Println("failed")
		fmt.Println("[!] failed to cache file")
		t.FailNow()
	}
	time.Sleep(1500 * time.Millisecond)
	contents, err := cache.ReadFile(testFile)
	if err != nil || !ValidateDataMatchesFile(contents, testFile) {
		fmt.Println("failed")
		fmt.Printf("[!] file read failed: ")
		if err != nil {
			fmt.Println(err.Error())
		} else {
			fmt.Println("cache contents do not match file")
		}
		t.FailNow()
	}
	time.Sleep(1 * time.Second)
	if !cache.InCache(testFile) {
		fmt.Println("failed")
		fmt.Println("[!] item should not have expired")
		t.Fail()
	} else {
		fmt.Println("ok")
	}
}

func TestFileChanged(t *testing.T) {
	fmt.Printf("[+] validate file modification expires item: ")
	cache := NewDefaultCache()
	if err := cache.Start(); err != nil {
		fmt.Println("failed")
		fmt.Println("[!] cache failed to start: ", err.Error())
		return
	}
	defer cache.Stop()

	name := writeTempFile(t, "lorem ipsum blah blah")
	if name == "" {
		fmt.Println("failed!")
		fmt.Println("[!] failed to cache item")
		t.FailNow()
	} else if err := cache.CacheNow(name); err != nil {
		fmt.Println("failed!")
		fmt.Println("[!] failed to cache item")
		t.FailNow()
	} else if !cache.InCache(name) {
		fmt.Println("failed")
		fmt.Println("[!] failed to cache item")
		os.Remove(name)
		t.FailNow()
	}
	time.Sleep(1 * time.Second)
	err := os.WriteFile(name, []byte("after modification"), 0600)
	if err != nil {
		fmt.Println("failed")
		fmt.Println("[!] couldn't write temporary file: ", err.Error())
		t.Fail()
	} else if !cache.changed(name) {
		fmt.Println("failed")
		fmt.Println("[!] item should have expired!")
		t.Fail()
	}
	os.Remove(name)
	fmt.Println("ok")
}

func TestCache(t *testing.T) {
	fmt.Printf("[+] testing asynchronous file caching: ")
	cache := NewDefaultCache()
	if err := cache.Start(); err != nil {
		fmt.Println("failed")
		fmt.Println("[!] cache failed to start: ", err.Error())
		return
	}
	defer cache.Stop()
	name := writeTempFile(t, "lorem ipsum akldfjsdlf")
	if name == "" {
		t.FailNow()
	} else if cache.InCache(name) {
		fmt.Println("failed")
		fmt.Println("[!] item should not be in cache yet!")
		os.Remove(name)
		t.FailNow()
	}

	cache.Cache(name, nil)

	var (
		delay int
		ok    bool
		dur   time.Duration
		step  = 10
		stop  = 500
	)
	dur, err := time.ParseDuration(fmt.Sprintf("%dµs", step))
	if err != nil {
		panic(err.Error())
	}

	for ok = cache.InCache(name); !ok; ok = cache.InCache(name) {
		time.Sleep(dur)
		delay += step
		if delay >= stop {
			break
		}
	}

	if !ok {
		fmt.Println("failed")
		fmt.Printf("\t[*] cache check stopped after %dµs\n", delay)
		t.Fail()
	} else {
		fmt.Println("ok")
		fmt.Printf("\t[*] item cached in %dµs\n", delay)
	}
	os.Remove(name)
}

func TestExpireAll(t *testing.T) {
	fmt.Printf("[+] testing background expiration: ")
	cache := NewDefaultCache()
	cache.Every = 1
	cache.ExpireItem = 2
	if err := cache.Start(); err != nil {
		fmt.Println("failed")
		fmt.Println("[!] cache failed to start: ", err.Error())
		return
	}
	defer cache.Stop()

	name := writeTempFile(t, "this is a first file and some stuff should go here")
	if name == "" {
		t.Fail()
	}
	name2 := writeTempFile(t, "this is the second file")
	if name2 == "" {
		os.Remove(name)
		t.Fail()
	}
	if t.Failed() {
		t.FailNow()
	}

	cache.CacheNow(name)
	time.Sleep(500 * time.Millisecond)
	cache.CacheNow(name2)
	time.Sleep(500 * time.Millisecond)

	err := os.WriteFile(name2, []byte("lorem ipsum dolor sit amet."), 0600)
	if err != nil {
		fmt.Println("failed")
		fmt.Println("[!] couldn't write temporary file: ", err.Error())
		t.FailNow()
	}

	if !t.Failed() {
		time.Sleep(1250 * time.Millisecond)
		if cache.Size() > 0 {
			fmt.Println("failed")
			fmt.Printf("[!] %d items still in cache", cache.Size())
			t.Fail()
		}
	}

	if !t.Failed() {
		fmt.Println("ok")
	}
	os.Remove(name)
	os.Remove(name2)
}

func destroyNames(names []string) {
	for _, name := range names {
		os.Remove(name)
	}
}

func TestExpireOldest(t *testing.T) {
	fmt.Printf("[+] validating item limit on cache: ")
	cache := NewDefaultCache()
	cache.MaxItems = 5
	if err := cache.Start(); err != nil {
		fmt.Println("failed")
		fmt.Println("[!] cache failed to start: ", err.Error())
		return
	}
	defer cache.Stop()

	names := make([]string, 0)
	for i := 0; i < 1000; i++ {
		name := writeTempFile(t, fmt.Sprintf("file number %d\n", i))
		if t.Failed() {
			break
		}
		names = append(names, name)
		cache.CacheNow(name)
	}

	if !t.Failed() && cache.Size() > cache.MaxItems {
		fmt.Println("failed")
		fmt.Printf("[!] %d items in cache (limit should be %d)",
			cache.Size(), cache.MaxItems)
		t.Fail()
	}
	if !t.Failed() {
		fmt.Println("ok")
	}
	destroyNames(names)
}

func TestNeverExpire(t *testing.T) {
	fmt.Printf("[+] validating no time limit expirations: ")
	cache := NewDefaultCache()
	cache.ExpireItem = 0
	cache.Every = 1
	if err := cache.Start(); err != nil {
		fmt.Println("failed")
		fmt.Println("[!] cache failed to start: ", err.Error())
		return
	}
	defer cache.Stop()

	tmpf, err := os.CreateTemp("", "fctest")
	if err != nil {
		fmt.Println("failed")
		fmt.Println("[!] couldn't create temporary file: ", err.Error())
		t.FailNow()
	}
	name := tmpf.Name()
	tmpf.Close()

	err = os.WriteFile(name, []byte("lorem ipsum dolor sit amet."), 0600)
	if err != nil {
		fmt.Println("failed")
		fmt.Println("[!] couldn't write temporary file: ", err.Error())
		os.Remove(name)
		t.FailNow()
	}
	cache.Cache(name, nil)
	time.Sleep(2 * time.Second)
	if !cache.InCache(name) {
		fmt.Println("failed")
		fmt.Println("[!] item should not have been expired")
		t.Fail()
	} else {
		fmt.Println("ok")
	}
	os.Remove(name)
}

func BenchmarkAsyncCaching(b *testing.B) {
	for i := 0; i < b.N; i++ {
		cache := NewDefaultCache()
		if err := cache.Start(); err != nil {
			fmt.Println("[!] cache failed to start: ", err.Error())
			return
		}
		defer cache.Stop()

		cache.Cache("filecache.go", nil)
		for {
			if cache.InCache("filecache.go") {
				break
			}
			<-time.After(200 * time.Microsecond)
		}
		cache.Remove("filecache.go")
	}
}

func TestCacheReadFile(t *testing.T) {
	fmt.Printf("[+] testing transparent file reads: ")
	testFile := "filecache.go"
	cache := NewDefaultCache()
	if err := cache.Start(); err != nil {
		fmt.Println("failed")
		fmt.Println("[!] cache failed to start: ", err.Error())
		return
	}
	defer cache.Stop()

	if cache.InCache(testFile) {
		fmt.Println("failed")
		fmt.Println("[!] file should not be in cache yet")
		t.FailNow()
	}

	out, err := cache.ReadFile(testFile)
	if (err != nil && err != ItemNotInCache) || !ValidateDataMatchesFile(out, testFile) {
		fmt.Println("failed")
		fmt.Printf("[!] transparent file read has failed: ")
		if err != nil {
			fmt.Println(err.Error())
		} else {
			fmt.Println("file does not match cache contents")
		}
		t.FailNow()
	}

	time.Sleep(10 * time.Millisecond)
	out, err = cache.ReadFile(testFile)
	if err != nil || !ValidateDataMatchesFile(out, testFile) {
		fmt.Println("failed")
		fmt.Println("[!] ReadFile has failed")
		t.Fail()
	} else {
		fmt.Println("ok")
	}
}

func BenchmarkSyncCaching(b *testing.B) {
	for i := 0; i < b.N; i++ {
		cache := NewDefaultCache()

		if err := cache.Start(); err != nil {
			fmt.Println("[!] cache failed to start: ", err.Error())
			return
		}
		defer cache.Stop()
		cache.CacheNow("filecache.go")
		cache.Remove("filecache.go")
	}
}

func ValidateDataMatchesFile(out []byte, filename string) bool {
	fileData, err := os.ReadFile(filename)
	if err != nil {
		return false
	} else if len(fileData) != len(out) {
		return false
	}

	for i := 0; i < len(out); i++ {
		if out[i] != fileData[i] {
			return false
		}
	}
	return true
}

func TestAccessCount(t *testing.T) {
	// add 100 items to the cache
	count := 100
	cache := NewDefaultCache() //Cache("testAccessCount")
	if err := cache.Start(); err != nil {
		fmt.Println("failed")
		fmt.Println("[!] cache failed to start: ", err.Error())
		return
	}
	defer cache.Stop()

	for i := 0; i < count; i++ {
		name := strconv.Itoa(i)
		itm := getTimeExpiredCacheItem()
		cache._add_cache_item(name, itm)
	}
	// never access the first item, access the second item once, the third
	// twice and so on...
	for i := 0; i < count; i++ {
		for j := 0; j < i; j++ {
			cache.GetItem(strconv.Itoa(i))
		}
	}

	// check MostAccessed returns the items in correct order
	ma := cache.MostAccessed(int64(count))
	fmt.Println("ma1", len(ma))
	for i, item := range ma {
		if k, _ := strconv.Atoi(item.Key()); k != count-1-i {
			t.Error("Most accessed items seem to be sorted incorrectly", k, count-1-i, item.Key())
		}
	}

	// check MostAccessed returns the correct amount of items
	want := count - 49
	ma = cache.MostAccessed(int64(want))
	if len(ma) != want {
		t.Error("MostAccessed returns incorrect amount of items:", len(ma), "want:", want)
	}
}

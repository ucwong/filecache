package filecache

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"
)

const VERSION = "1.0.0"

// File size constants for use with FileCache.MaxSize.
// For example, cache.MaxSize = 64 * Megabyte
const (
	Kilobyte = 1024
	Megabyte = 1024 * 1024
	Gigabyte = 1024 * 1024 * 1024
)

var (
	DefaultExpireItem int   = 300 // 5 minutes
	DefaultMaxSize    int64 = 16 * Megabyte
	DefaultMaxItems   int   = 32
	DefaultEvery      int   = 60 // 1 minute
)

var (
	InvalidCacheItem = errors.New("invalid cache item")
	ItemIsDirectory  = errors.New("can't cache a directory")
	ItemNotInCache   = errors.New("item not in cache")
	ItemTooLarge     = errors.New("item too large for cache")
	WriteIncomplete  = errors.New("incomplete write of cache item")
)

var SquelchItemNotInCache = true

// Mumber of items to buffer adding to the file cache.
var NewCachePipeSize = 1

type cacheItem struct {
	content    []byte
	lock       sync.RWMutex
	Size       int64
	Lastaccess time.Time
	Modified   time.Time
}

func (itm *cacheItem) WasModified(fi os.FileInfo) bool {
	itm.lock.RLock()
	defer itm.lock.RUnlock()
	return itm.Modified.Equal(fi.ModTime())
}

func (itm *cacheItem) GetReader() (b io.Reader) {
	itm.lock.Lock()
	defer itm.lock.Unlock()
	b = bytes.NewReader(itm.Access())
	return
}

func (itm *cacheItem) Access() (c []byte) {
	itm.lock.Lock()
	defer itm.lock.Unlock()
	itm.Lastaccess = time.Now()
	c = itm.content
	return
}

func (itm *cacheItem) Dur() (t time.Duration) {
	itm.lock.RLock()
	defer itm.lock.RUnlock()
	t = time.Now().Sub(itm.Lastaccess)
	return
}

// FileCache represents a cache in memory.
// An ExpireItem value of 0 means that items should not be expired based
// on time in memory.
type FileCache struct {
	dur        time.Duration
	items      map[string]*cacheItem
	in         chan *CacheInfo
	mutex      sync.RWMutex
	shutdown   chan any
	wait       sync.WaitGroup
	MaxItems   int   // Maximum number of files to cache
	MaxSize    int64 // Maximum file size to store
	ExpireItem int   // Seconds a file should be cached for
	Every      int   // Run an expiration check Every seconds
}

type CacheInfo struct {
	name    string
	content []byte
}

// NewDefaultCache returns a new FileCache with sane defaults.
func NewDefaultCache() *FileCache {
	return &FileCache{
		dur:        time.Since(time.Now()),
		items:      nil,
		in:         nil,
		MaxItems:   DefaultMaxItems,
		MaxSize:    DefaultMaxSize,
		ExpireItem: DefaultExpireItem,
		Every:      DefaultEvery,
	}
}

func (cache *FileCache) lock() {
	cache.mutex.Lock()
}

func (cache *FileCache) unlock() {
	cache.mutex.Unlock()
}

func (cache *FileCache) rlock() {
	cache.mutex.RLock()
}

func (cache *FileCache) runlock() {
	cache.mutex.RUnlock()
}

func (cache *FileCache) isCacheNull() bool {
	cache.rlock()
	defer cache.runlock()
	return cache.items == nil
}

func (cache *FileCache) getItem(name string) (itm *cacheItem, ok bool) {
	if cache.isCacheNull() {
		return nil, false
	}
	cache.rlock()
	defer cache.runlock()
	itm, ok = cache.items[name]
	return
}

// addItem is an internal function for adding an item to the cache.
func (cache *FileCache) addItem(name string, content []byte) (err error) {
	if cache.isCacheNull() {
		return
	}
	ok := cache.InCache(name)
	expired := cache.itemExpired(name)
	if ok && !expired {
		return
	} else if ok {
		cache.deleteItem(name)
	}

	itm, err := cacheFile(name, cache.MaxSize, content)
	cache.lock()
	if cache.items != nil && itm != nil {
		cache.items[name] = itm
		cache.unlock()
	} else {
		cache.unlock()
		return
	}
	if !cache.InCache(name) {
		err = ItemNotInCache
	}
	return
}

func (cache *FileCache) deleteItem(name string) {
	cache.lock()
	delete(cache.items, name)
	cache.unlock()
}

// itemListener is a goroutine that listens for incoming files and caches
// them.
func (cache *FileCache) itemListener() {
	defer cache.wait.Done()
	for {
		select {
		case c := <-cache.in:
			cache.addItem(c.name, c.content)
		case <-cache.shutdown:
			return
		}
	}
}

// expireOldest is used to expire the oldest item in the cache.
// The force argument is used to indicate it should remove at least one
// entry; for example, if a large number of files are cached at once, none
// may appear older than another.
func (cache *FileCache) expireOldest(force bool) {
	oldest := time.Now()
	oldestName := ""

	for name, itm := range cache.items {
		if (force && oldestName == "") || itm.Lastaccess.Before(oldest) {
			oldest = itm.Lastaccess
			oldestName = name
		}
	}
	if oldestName != "" {
		cache.deleteItem(oldestName)
	}
}

// vacuum is a background goroutine responsible for cleaning the cache.
// It runs periodically, every cache.Every seconds. If cache.Every is set
// to 0, it will not run.
func (cache *FileCache) vacuum() {
	defer cache.wait.Done()
	if cache.Every < 1 {
		return
	}

	t := time.NewTicker(cache.dur)
	defer t.Stop()
	for {
		select {
		case <-cache.shutdown:
			return
		case <-t.C:
			if cache.isCacheNull() {
				return
			}
			for name := range cache.items {
				if cache.itemExpired(name) {
					cache.deleteItem(name)
				}
			}
			for size := cache.Size(); size > cache.MaxItems; size = cache.Size() {
				cache.expireOldest(true)
			}
		}
	}
}

// FileChanged returns true if file should be expired based on mtime.
// If the file has changed on disk or no longer exists, it should be
// expired.
func (cache *FileCache) changed(name string) bool {
	itm, ok := cache.getItem(name)
	if !ok || itm == nil {
		return true
	}
	fi, err := os.Stat(name)
	if err != nil || !itm.WasModified(fi) {
		return true
	}
	return false
}

// Expired returns true if the item has not been accessed recently.
func (cache *FileCache) expired(name string) bool {
	itm, ok := cache.getItem(name)
	if !ok {
		return true
	}
	dur := itm.Dur()
	sec, err := strconv.Atoi(fmt.Sprintf("%0.0f", dur.Seconds()))
	if err != nil || sec >= cache.ExpireItem {
		return true
	}
	return false
}

// itemExpired returns true if an item is expired.
func (cache *FileCache) itemExpired(name string) bool {
	if cache.changed(name) || (cache.ExpireItem != 0 && cache.expired(name)) {
		return true
	}
	return false
}

// Active returns true if the cache has been started, and false otherwise.
func (cache *FileCache) Active() bool {
	if cache.in == nil || cache.isCacheNull() {
		return false
	}
	return true
}

// Size returns the number of entries in the cache.
func (cache *FileCache) Size() int {
	cache.rlock()
	defer cache.runlock()
	return len(cache.items)
}

// FileSize returns the sum of the file sizes stored in the cache
func (cache *FileCache) FileSize() (totalSize int64) {
	cache.rlock()
	defer cache.runlock()
	for _, itm := range cache.items {
		totalSize += itm.Size
	}
	return
}

// StoredFiles returns the list of files stored in the cache.
func (cache *FileCache) StoredFiles() (fileList []string) {
	fileList = make([]string, 0, cache.Size())
	if cache.isCacheNull() || cap(fileList) == 0 {
		return
	}

	cache.rlock()
	defer cache.runlock()
	for name := range cache.items {
		fileList = append(fileList, name)
	}
	return
}

// InCache returns true if the item is in the cache.
func (cache *FileCache) InCache(name string) bool {
	if cache.changed(name) {
		cache.deleteItem(name)
		return false
	}
	cache.rlock()
	defer cache.runlock()
	_, ok := cache.items[name]
	return ok
}

// WriteItem writes the cache item to the specified io.Writer.
func (cache *FileCache) WriteItem(w io.Writer, name string) (err error) {
	itm, ok := cache.getItem(name)
	if !ok {
		if !SquelchItemNotInCache {
			err = ItemNotInCache
		}
		return
	}

	r := itm.GetReader()
	itm.Lastaccess = time.Now()
	n, err := io.Copy(w, r)
	if err != nil {
		return
	} else if int64(n) != itm.Size {
		err = WriteIncomplete
		return
	}
	return
}

// GetItem returns the content of the item and a bool if name is present.
// GetItem should be used when you are certain an object is in the cache,
// or if you want to use the cache only.
func (cache *FileCache) GetItem(name string) (content []byte, ok bool) {
	itm, ok := cache.getItem(name)
	if !ok {
		return
	}
	content = itm.Access()
	return
}

// GetItemString is the same as GetItem, except returning a string.
func (cache *FileCache) GetItemString(name string) (content string, ok bool) {
	itm, ok := cache.getItem(name)
	if !ok {
		return
	}
	content = string(itm.Access())
	return
}

// ReadFileString is the same as ReadFile, except returning a string.
func (cache *FileCache) ReadFileString(name string) (content string, err error) {
	raw, err := cache.ReadFile(name)
	if err == nil {
		content = string(raw)
	}
	return
}

// WriteFile writes the file named by 'name' to the specified io.Writer.
// If the file is in the cache, it is loaded from the cache; otherwise,
// it is read from the filesystem and the file is cached in the background.
func (cache *FileCache) WriteFile(w io.Writer, name string) (err error) {
	if cache.InCache(name) {
		err = cache.WriteItem(w, name)
	} else {
		var fi os.FileInfo
		fi, err = os.Stat(name)
		if err != nil {
			return
		} else if fi.IsDir() {
			return ItemIsDirectory
		}
		cache.Cache(name, nil)
		var file *os.File
		file, err = os.Open(name)
		if err != nil {
			return
		}
		defer file.Close()
		_, err = io.Copy(w, file)
	}
	return
}

// Cache will store the file named by 'name' to the cache.
// This function doesn't return anything as it passes the file onto the
// incoming pipe; the file will be cached asynchronously. Errors will
// not be returned.
func (cache *FileCache) Cache(name string, content []byte) {
	if cache.Size() == cache.MaxItems {
		cache.expireOldest(true)
	}
	cache.in <- &CacheInfo{name, content}
}

// CacheNow immediately caches the file named by 'name'.
func (cache *FileCache) CacheNow(name string) (err error) {
	if cache.Size() == cache.MaxItems {
		cache.expireOldest(true)
	}
	return cache.addItem(name, nil)
}

// Start activates the file cache; it will start up the background caching
// and automatic cache expiration goroutines and initialise the internal
// data structures.
func (cache *FileCache) Start() error {
	dur, err := time.ParseDuration(fmt.Sprintf("%ds", cache.Every))
	if err != nil {
		return err
	}
	cache.dur = dur
	cache.items = make(map[string]*cacheItem, 0)
	cache.in = make(chan *CacheInfo, NewCachePipeSize)
	cache.shutdown = make(chan any)

	cache.wait.Add(2)
	go cache.itemListener()
	go cache.vacuum()
	return nil
}

// Stop turns off the file cache.
// This closes the concurrent caching mechanism, destroys the cache, and
// the background scanner that it should stop.
// If there are any items or cache operations ongoing while Stop() is called,
// it is undefined how they will behave.
func (cache *FileCache) Stop() {
	close(cache.shutdown)

	cache.wait.Wait()

	if cache.items != nil {
		items := cache.StoredFiles()
		for _, name := range items {
			cache.deleteItem(name)
		}
		cache.lock()
		cache.items = nil
		cache.unlock()
	}
}

// RemoveItem immediately removes the item from the cache if it is present.
// It returns a boolean indicating whether anything was removed, and an error
// if an error has occurred.
func (cache *FileCache) Remove(name string) (ok bool, err error) {
	_, ok = cache.items[name]
	if !ok {
		return
	}
	cache.deleteItem(name)
	_, valid := cache.getItem(name)
	if valid {
		ok = false
	}
	return
}

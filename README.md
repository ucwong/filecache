# filecache
## a simple file cache of Golang

## Install

First, you need to install the package:

```
go get -u github.com/ucwong/filecache
```

## Overview

A file cache can be created with either the `NewDefaultCache()` function to
get a cache with the defaults set, or `NewCache()` to get a new cache with
`0` values for everything; you will not be able to store items in this cache
until the values are changed; specifically, at a minimum, you should set
the `MaxItems` field to be > 0.

Let's start with a basic example; we'll create a basic cache and give it a
maximum item size of 128M:

```
cache := filecache.NewDefaultCache()
cache.MaxSize = 128 * filecache.Megabyte
cache.Start()
```

The `Kilobyte`, `Megabyte`, and `Gigabyte` constants are provided as a
convience when setting cache sizes.

When `cache.Start()` is called, a goroutine is launched in the background
that routinely checks the cache for expired items. The delay between
runs is specified as the number of seconds given by `cache.Every` ("every
`cache.Every` seconds, check for expired items"). There are three criteria
used to determine whether an item in the cache should be expired; they are:

   1. Has the file been modified on disk? (The cache stores the last time
      of modification at the time of caching, and compares that to the
      file's current last modification time).
   2. Has the file been in the cache for longer than the maximum allowed
      time?
   3. Is the cache at capacity? When a file is being cached, a check is
      made to see if the cache is currently filled. If it is, the item that
      was last accessed the longest ago is expired and the new item takes
      its place. When loading items asynchronously, this check might miss
      the fact that the cache will be at capacity; the background scanner
      performs a check after its regular checks to ensure that the cache is
      not at capacity.

The background scanner can be disabled by setting `cache.Every` to 0; if so,
cache expiration is only done when the cache is at capacity.

Once the cache is no longer needed, a call to `cache.Stop()` will close down
the channels and signal the background scanner that it should stop.


## Usage

### Initialisation and Startup

The public fields of the `FileCache` struct are:

```
    MaxItems   int   // Maximum number of files to cache
    MaxSize    int64 // Maximum file size to store
    ExpireItem int   // Seconds a file should be cached for
    Every      int   // Run an expiration check Every seconds
```

You can create a new file cache with one of two functions:

* `NewCache()`: creates a new bare repository that just has the underlying
cache structure initialised. The public fields are all set to `0`, which is
very likely not useful (at a minimum, a `MaxItems` of `0` means no items can
or will be stored in the cache).
* `NewDefaultCache()` returns a new file cache initialised to some basic
defaults. The defaults are:

```
	DefaultExpireItem int   = 300 // 5 minutes
	DefaultMaxSize    int64 =  4 * Megabyte
	DefaultMaxItems   int   = 32
	DefaultEvery      int   = 60 // 1 minute
```

These defaults are public variables, and you may change them to more useful
values to your program.

Once the cache has been initialised, it needs to be started using the
`Start()` method. This is important for initialising the goroutine responsible
for ensuring the cache remains updated, as well as setting up the asynchronous
caching goroutine. The `Active` method returns true if the cache is currently
running. `Start()` returns an `error` if an error occurs; if one is returned,
the cache should not be used.

package backtracker

import (
	"bufio"
	"io"

	"github.com/pkg/errors"
)

// Backtracker allows reads from an io.Reader while remembering
// the last N bytes of data. Backtrack() can then be called, to
// read those bytes again.
type Backtracker interface {
	io.Reader

	// Returns the current offset (doesn't count backtracking)
	Offset() int64

	// Return amount of bytes that can be backtracked
	Cached() int64

	// Backtrack n bytes
	Backtrack(n int64) error

	// Advance n bytes
	Discard(n int64) error

	NumCacheHits() int64
	NumCacheMiss() int64

	CachedBytesServed() int64
	TotalBytesServed() int64
}

// New returns a Backtracker reading from upstream
func New(offset int64, upstream io.Reader, cacheSize int64) Backtracker {
	return &backtracker{
		upstream:   bufio.NewReader(upstream),
		discardBuf: make([]byte, 256*1024),
		cache:      make([]byte, cacheSize),
		cached:     0,
		backtrack:  0,
		offset:     offset,
	}
}

type backtracker struct {
	upstream    *bufio.Reader
	cache       []byte
	discardBuf  []byte
	writeCursor int
	cached      int
	backtrack   int
	offset      int64

	numCacheHits      int64
	numCacheMiss      int64
	cachedBytesServed int64
	totalBytesServed  int64
}

func (bt *backtracker) NumCacheHits() int64 {
	return bt.numCacheHits
}

func (bt *backtracker) NumCacheMiss() int64 {
	return bt.numCacheMiss
}

func (bt *backtracker) CachedBytesServed() int64 {
	return bt.cachedBytesServed
}

func (bt *backtracker) TotalBytesServed() int64 {
	return bt.totalBytesServed
}

var _ Backtracker = (*backtracker)(nil)

func (bt *backtracker) Read(buf []byte) (int, error) {
	n := len(buf)
	cachesize := len(bt.cache)

	// read from cache
	if bt.backtrack > 0 {
		if n > bt.backtrack {
			n = bt.backtrack
		}

		// [LLLL).........RRRRRRRR]
		//      |woff
		//   L            R

		readstart := bt.writeCursor - bt.backtrack
		if readstart < 0 {
			readstart += cachesize
			if n > cachesize-readstart {
				n = cachesize - readstart
			}
			// we'll read that first:
			// [....).........RRRRRRRR]
			//
			// then cachestart will be 0
			// and we'll read this:
			// [LLLL).................]
		}

		copy(buf[:n], bt.cache[readstart:])
		bt.backtrack -= n
		bt.numCacheHits++
		bt.cachedBytesServed += int64(n)
		bt.totalBytesServed += int64(n)
		return n, nil
	}

	bt.numCacheMiss++

	// read from upstream
	n, err := bt.upstream.Read(buf)

	if n > 0 {
		bt.offset += int64(n)

		if cachesize > 0 {
			// cache data
			cachebytes := buf[:n]
			if n > cachesize {
				cachebytes = buf[n-cachesize:]
			}

			remain := cachesize - bt.writeCursor
			copy(bt.cache[bt.writeCursor:], cachebytes)

			if len(cachebytes) > remain {
				copy(bt.cache, cachebytes[remain:])
			}
			bt.writeCursor = (bt.writeCursor + len(cachebytes)) % cachesize

			bt.cached += n
			if bt.cached > cachesize {
				bt.cached = cachesize
			}
		}
	}

	bt.totalBytesServed += int64(n)
	return n, err
}

func (bt *backtracker) Discard(n int64) error {
	discardlen := int64(len(bt.discardBuf))

	for n > 0 {
		readlen := n
		if readlen > discardlen {
			readlen = discardlen
		}

		discarded, err := bt.Read(bt.discardBuf[:readlen])
		if err != nil {
			return errors.WithMessage(err, "discarding")
		}

		n -= int64(discarded)
	}
	return nil
}

func (bt *backtracker) Cached() int64 {
	return int64(bt.cached)
}

func (bt *backtracker) Backtrack(n int64) error {
	if int64(bt.cached) < n {
		return errors.Errorf("only %d cached, can't backtrack by %d", bt.cached, n)
	}
	bt.backtrack = int(n)
	return nil
}

func (bt *backtracker) Offset() int64 {
	return bt.offset
}

/*
---------------------------------------------------

// [)                     ]
// |woff
// backtrack=0

// [.....)                ]
//       |woff
// <-----|
//   backtrack

// [..................)   ]
//                    |woff
// <------------------|
//      backtrack

// [....).................]
//      |woff
//  ----|<----------------
//  k             backtrac

---------------------------------------------------
*/

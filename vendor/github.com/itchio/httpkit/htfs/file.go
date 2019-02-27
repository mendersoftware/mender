package htfs

import (
	"fmt"
	"io"
	"log"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	goerrors "errors"

	"github.com/itchio/httpkit/neterr"
	"github.com/itchio/httpkit/progress"
	"github.com/itchio/httpkit/retrycontext"
	"github.com/pkg/errors"
)

var forbidBacktracking = os.Getenv("HTFS_NO_BACKTRACK") == "1"
var dumpStats = os.Getenv("HTFS_DUMP_STATS") == "1"

// A GetURLFunc returns a URL we can download the resource from.
// It's handy to have this as a function rather than a constant for signed expiring URLs
type GetURLFunc func() (urlString string, err error)

// A NeedsRenewalFunc analyzes an HTTP response and returns true if it needs to be renewed
type NeedsRenewalFunc func(res *http.Response, body []byte) bool

// A LogFunc prints debug message
type LogFunc func(msg string)

// amount we're willing to download and throw away
const maxDiscard int64 = 1 * 1024 * 1024 // 1MB

const maxRenewals = 5

// ErrNotFound is returned when the HTTP server returns 404 - it's not considered a temporary error
var ErrNotFound = goerrors.New("HTTP file not found on server")

// ErrTooManyRenewals is returned when we keep calling the GetURLFunc but it
// immediately return an errors marked as renewal-related by NeedsRenewalFunc.
// This can happen when servers are misconfigured.
var ErrTooManyRenewals = goerrors.New("Giving up, getting too many renewals. Try again later or contact support.")

type hstats struct {
	// this needs to be 64-bit aligned
	fetchedBytes int64
	cachedBytes  int64

	numCacheMiss int64
	numCacheHits int64

	connectionWait time.Duration
	connections    int
	expired        int
	renews         int
}

var idSeed int64 = 1
var idMutex sync.Mutex

// File allows accessing a file served by an HTTP server as if it was local
// (for random-access reading purposes, not writing)
type File struct {
	getURL        GetURLFunc
	needsRenewal  NeedsRenewalFunc
	client        *http.Client
	retrySettings *retrycontext.Settings

	Log      LogFunc
	LogLevel int

	name   string
	size   int64
	offset int64 // for io.ReadSeeker

	ConnStaleThreshold time.Duration
	MaxConns           int

	closed bool

	conns     map[string]*conn
	connsLock sync.Mutex

	currentURL string
	urlMutex   sync.Mutex
	header     http.Header
	requestURL *url.URL

	stats *hstats

	ForbidBacktracking bool
	DumpStats          bool
}

const defaultLogLevel = 1

// defaultConnStaleThreshold is the duration after which File's conns
// are considered stale, and are closed instead of reused. It's set to 10 seconds.
const defaultConnStaleThreshold = time.Second * time.Duration(10)

var _ io.Seeker = (*File)(nil)
var _ io.Reader = (*File)(nil)
var _ io.ReaderAt = (*File)(nil)
var _ io.Closer = (*File)(nil)

// Settings allows passing additional settings to an File
type Settings struct {
	Client             *http.Client
	RetrySettings      *retrycontext.Settings
	Log                LogFunc
	LogLevel           int
	ForbidBacktracking bool
	DumpStats          bool
}

// Open returns a new htfs.File. Note that it differs from os.Open in that it does a first request
// to determine the remote file's size. If that fails (after retries), an error will be returned.
func Open(getURL GetURLFunc, needsRenewal NeedsRenewalFunc, settings *Settings) (*File, error) {
	client := settings.Client
	if client == nil {
		client = http.DefaultClient
	}

	retryCtx := retrycontext.NewDefault()
	if settings.RetrySettings != nil {
		retryCtx.Settings = *settings.RetrySettings
	}

	f := &File{
		getURL:        getURL,
		retrySettings: &retryCtx.Settings,
		needsRenewal:  needsRenewal,
		client:        client,
		name:          "<remote file>",

		conns: make(map[string]*conn),
		stats: &hstats{},

		ConnStaleThreshold: defaultConnStaleThreshold,
		LogLevel:           defaultLogLevel,
		ForbidBacktracking: forbidBacktracking,
		DumpStats:          dumpStats,
		// number obtained through gut feeling
		// may not be suitable to all workloads
		MaxConns: 8,
	}
	f.Log = settings.Log

	if settings.LogLevel != 0 {
		f.LogLevel = settings.LogLevel
	}
	if settings.ForbidBacktracking {
		f.ForbidBacktracking = true
	}
	if settings.DumpStats {
		f.DumpStats = true
	}

	urlStr, err := getURL()
	if err != nil {
		return nil, errors.WithMessage(normalizeError(err), "htfs.Open (getting URL)")
	}
	f.currentURL = urlStr

	c, err := f.borrowConn(0)
	if err != nil {
		return nil, errors.WithMessage(normalizeError(err), "htfs.Open (initial request)")
	}
	f.header = c.header

	err = f.returnConn(c)
	if err != nil {
		return nil, errors.WithMessage(normalizeError(err), "htfs.Open (return conn after initial request)")
	}

	f.requestURL = c.requestURL

	if c.statusCode == 206 {
		rangeHeader := c.header.Get("content-range")
		rangeTokens := strings.Split(rangeHeader, "/")
		totalBytesStr := rangeTokens[len(rangeTokens)-1]
		f.size, err = strconv.ParseInt(totalBytesStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("Could not parse file size: %s", err.Error())
		}
	} else if c.statusCode == 200 {
		f.size = c.contentLength
	}

	// we have to use requestURL because we want the URL after
	// redirect (for hosts like sourceforge)
	pathTokens := strings.Split(f.requestURL.Path, "/")
	f.name = pathTokens[len(pathTokens)-1]

	dispHeader := c.header.Get("content-disposition")
	if dispHeader != "" {
		_, mimeParams, err := mime.ParseMediaType(dispHeader)
		if err == nil {
			filename := mimeParams["filename"]
			if filename != "" {
				f.name = filename
			}
		}
	}

	return f, nil
}

func (f *File) newRetryContext() *retrycontext.Context {
	retryCtx := retrycontext.NewDefault()
	if f.retrySettings != nil {
		retryCtx.Settings = *f.retrySettings
	}
	return retryCtx
}

// NumConns returns the number of connections currently used by the File
// to serve ReadAt calls
func (f *File) NumConns() int {
	f.connsLock.Lock()
	defer f.connsLock.Unlock()

	return len(f.conns)
}

func (f *File) borrowConn(offset int64) (*conn, error) {
	f.connsLock.Lock()
	defer f.connsLock.Unlock()

	if f.knownSize() && offset >= f.size {
		return nil, io.EOF
	}

	var bestConn string
	var bestDiff int64 = math.MaxInt64

	var bestBackConn string
	var bestBackDiff int64 = math.MaxInt64

	for _, c := range f.conns {
		if c.Stale() {
			f.stats.expired++
			err := f.closeConn(c)
			if err != nil {
				return nil, err
			}
			continue
		}

		diff := offset - c.Offset()
		if diff < 0 && -diff < maxDiscard && -diff <= c.Cached() {
			if -diff < bestBackDiff {
				bestBackConn = c.id
				bestBackDiff = -diff
			}
		}

		if diff >= 0 && diff < maxDiscard {
			if diff < bestDiff {
				bestConn = c.id
				bestDiff = diff
			}
		}
	}

	if bestConn != "" {
		// re-use!
		c := f.conns[bestConn]
		delete(f.conns, bestConn)

		// clear backtrack if any
		c.Backtrack(0)

		// discard if needed
		if bestDiff > 0 {
			f.log2("[%9d-%9d] (Borrow) %d --> %d (%s)", offset, offset, c.Offset(), c.Offset()+bestDiff, c.id)

			err := c.Discard(bestDiff)
			if err != nil {
				if f.shouldRetry(err) {
					f.log2("[%9d-] (Borrow) discard failed, reconnecting", offset)
					err = c.Connect(offset)
					if err != nil {
						return nil, err
					}
				} else {
					return nil, err
				}
			}
		}

		return c, nil
	}

	if !f.ForbidBacktracking && bestBackConn != "" {
		// re-use!
		c := f.conns[bestBackConn]
		delete(f.conns, bestBackConn)

		f.log2("[%9d-%9d] (Borrow) %d <-- %d (%s)", offset, offset, c.Offset()-bestBackDiff, c.Offset(), c.id)

		// backtrack as needed
		err := c.Backtrack(bestBackDiff)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		return c, nil
	}

	// provision a new reader
	f.log("[%9d-%9d] (Borrow) new connection", offset, offset)

	id := generateID()
	c := &conn{
		file:      f,
		id:        fmt.Sprintf("reader-%d", id),
		touchedAt: time.Now(),
	}

	err := c.Connect(offset)
	if err != nil {
		return nil, err
	}

	return c, nil
}

type agedConn struct {
	id  string
	age time.Duration
}

func (f *File) returnConn(c *conn) error {
	f.connsLock.Lock()
	defer f.connsLock.Unlock()

	c.touchedAt = time.Now()
	f.conns[c.id] = c

	if len(f.conns)*2 > f.MaxConns*3 {
		var agedConns []agedConn
		for id, c := range f.conns {
			agedConns = append(agedConns, agedConn{id: id, age: time.Since(c.touchedAt)})
		}
		sort.Slice(agedConns, func(i, j int) bool {
			return agedConns[i].age < agedConns[j].age
		})

		victims := agedConns[f.MaxConns:]
		for _, ac := range victims {
			err := f.closeConn(f.conns[ac.id])
			if err != nil {

			}
		}
	}
	return nil
}

func (f *File) getCurrentURL() string {
	f.urlMutex.Lock()
	defer f.urlMutex.Unlock()

	return f.currentURL
}

func (f *File) renewURL() (string, error) {
	f.urlMutex.Lock()
	defer f.urlMutex.Unlock()

	urlStr, err := f.getURL()
	if err != nil {
		return "", err
	}

	f.currentURL = urlStr
	return f.currentURL, nil
}

// Stat returns an os.FileInfo for this particular file. Only the Size()
// method is useful, the rest is default values.
func (f *File) Stat() (os.FileInfo, error) {
	return &FileInfo{f}, nil
}

// Seek the read head within the file - it's instant and never returns an
// error, except if whence is one of os.SEEK_SET, os.SEEK_END, or os.SEEK_CUR.
// If an invalid offset is given, it will be truncated to a valid one, between
// [0,size).
func (f *File) Seek(offset int64, whence int) (int64, error) {
	var newOffset int64

	switch whence {
	case os.SEEK_SET:
		newOffset = offset
	case os.SEEK_END:
		newOffset = f.size + offset
	case os.SEEK_CUR:
		newOffset = f.offset + offset
	default:
		return f.offset, fmt.Errorf("invalid whence value %d", whence)
	}

	if newOffset < 0 {
		newOffset = 0
	}

	if newOffset > f.size {
		newOffset = f.size
	}

	f.offset = newOffset
	return f.offset, nil
}

func (f *File) Read(buf []byte) (int, error) {
	initialOffset := f.offset
	bytesRead, err := f.readAt(buf, f.offset)
	f.offset += int64(bytesRead)

	if f.LogLevel >= 2 {
		bytesWanted := int64(len(buf))
		start := initialOffset
		end := initialOffset + bytesWanted
		f.log2("[%9d-%9d] (Read) %d/%d %v", start, end, bytesRead, bytesWanted, err)
	}
	return bytesRead, err
}

// ReadAt reads len(buf) byte from the remote file at offset.
// It returns the number of bytes read, and an error. In case of temporary
// network errors or timeouts, it will retry with truncated exponential backoff
// according to RetrySettings
func (f *File) ReadAt(buf []byte, offset int64) (int, error) {
	bytesRead, err := f.readAt(buf, offset)

	if f.LogLevel >= 2 {
		bytesWanted := int64(len(buf))
		start := offset
		end := offset + bytesWanted

		var readDesc string
		if bytesWanted == int64(bytesRead) {
			readDesc = "full"
		} else if bytesRead == 0 {
			readDesc = fmt.Sprintf("partial (%d of %d)", bytesRead, bytesWanted)
		} else {
			readDesc = "zero"
		}
		if err != nil {
			readDesc += fmt.Sprintf(", with err %v", err)
		}
		f.log2("[%9d-%9d] (ReadAt) %s", start, end, readDesc)
	}
	return bytesRead, err
}

func (f *File) readAt(data []byte, offset int64) (int, error) {
	buflen := len(data)
	if buflen == 0 {
		return 0, nil
	}

	c, err := f.borrowConn(offset)
	if err != nil {
		return 0, err
	}
	// TODO: this swallows returnConn errors
	defer f.returnConn(c)

	totalBytesRead := 0
	bytesToRead := len(data)

	for totalBytesRead < bytesToRead {
		bytesRead, err := c.Read(data[totalBytesRead:])
		totalBytesRead += bytesRead

		if err != nil {
			// so, EOF can indicate connection reset sometimes
			// (see https://github.com/itchio/butler/issues/167)
			isEOF := errors.Cause(err) == io.EOF
			if isEOF && f.knownSize() {
				position := offset + int64(totalBytesRead)
				if position >= f.size {
					// ok, we've read up until the end of the file
					// so this must be a real EOF.
					return totalBytesRead, io.EOF
				}
			}

			if f.shouldRetry(err) {
				// for servers that don't support range requests
				// *and* don't specify the content-length header,
				// this will retry a bunch of times before returning
				// EOF, which is less than ideal, but in my defense,
				// screw those servers.
				f.log("Got %s, retrying", err.Error())
				err = c.Connect(c.Offset())
				if err != nil {
					return totalBytesRead, err
				}
			} else {
				return totalBytesRead, err
			}
		}
	}

	return totalBytesRead, nil
}

func (f *File) shouldRetry(err error) bool {
	if errors.Cause(err) == io.EOF {
		// *do* retry EOF, because apparently it's used interchangeably with
		// 'connection reset' in golang, see https://github.com/itchio/butler/issues/167
		return true
	}

	if neterr.IsNetworkError(err) {
		if strings.Contains(fmt.Sprintf("%v", err), "simulated offline") {
			// don't retry simulated offline
			return false
		}

		f.log("Retrying: %v", err)
		return true
	}

	if se, ok := errors.Cause(err).(*ServerError); ok {
		switch se.StatusCode {
		case 429: /* Too Many Requests */
			return true
		case 500: /* Internal Server Error */
			return true
		case 502: /* Bad Gateway */
			return true
		case 503: /* Service Unavailable */
			return true
		}
	}

	f.log("Bailing on error: %v", err)
	return false
}

func isHTTPStatus(err error, statusCode int) bool {
	if se, ok := errors.Cause(err).(*ServerError); ok {
		return se.StatusCode == statusCode
	}
	return false
}

func normalizeError(err error) error {
	if isHTTPStatus(err, 404) {
		return ErrNotFound
	}
	return err
}

func (f *File) closeAllConns() error {
	for _, c := range f.conns {
		err := f.closeConn(c)
		if err != nil {
			return err
		}
	}

	return nil
}

func (f *File) closeConn(c *conn) error {
	delete(f.conns, c.id)

	if f.DumpStats {
		f.stats.numCacheHits += c.NumCacheHits()
		f.stats.numCacheMiss += c.NumCacheMiss()
		f.stats.cachedBytes += c.CachedBytesServed()
		f.stats.fetchedBytes += c.TotalBytesServed()
	}
	return c.Close()
}

// Close closes all connections to the distant http server used by this File
func (f *File) Close() error {
	f.connsLock.Lock()
	defer f.connsLock.Unlock()

	if f.closed {
		return nil
	}

	err := f.closeAllConns()
	if err != nil {
		return err
	}

	if f.DumpStats {
		fetchedBytes := f.stats.fetchedBytes

		log.Printf("====== htfs stats for %s", f.name)
		log.Printf("= conns: %d total, %d expired, %d renews, wait %s", f.stats.connections, f.stats.expired, f.stats.renews, f.stats.connectionWait)
		size := f.size
		perc := 0.0
		percCached := 0.0
		if size != 0 {
			perc = float64(fetchedBytes) / float64(size) * 100.0
		}
		totalServedBytes := fetchedBytes
		percCached = float64(f.stats.cachedBytes) / float64(totalServedBytes) * 100.0

		log.Printf("= fetched: %s / %s (%.2f%%)", progress.FormatBytes(fetchedBytes), progress.FormatBytes(size), perc)
		log.Printf("= served from cache: %s (%.2f%% of all served bytes)", progress.FormatBytes(f.stats.cachedBytes), percCached)

		totalReads := f.stats.numCacheHits + f.stats.numCacheMiss
		if totalReads == 0 {
			totalReads = -1 // avoid NaN hit rate
		}
		hitRate := float64(f.stats.numCacheHits) / float64(totalReads) * 100.0
		log.Printf("= cache hit rate: %.2f%% (out of %d reads)", hitRate, totalReads)
		log.Printf("========================================")
	}

	f.closed = true

	return nil
}

func (f *File) knownSize() bool {
	return f.size > 0
}

func (f *File) log(format string, args ...interface{}) {
	if f.Log == nil {
		return
	}

	f.Log(fmt.Sprintf(format, args...))
}

func (f *File) log2(format string, args ...interface{}) {
	if f.LogLevel < 2 {
		return
	}

	if f.Log == nil {
		return
	}

	f.Log(fmt.Sprintf(format, args...))
}

// GetHeader returns the header the server responded
// with on our initial request. It may contain checksums
// which could be used for integrity checking.
func (f *File) GetHeader() http.Header {
	return f.header
}

// GetRequestURL returns the first good URL File
// made a request to.
func (f *File) GetRequestURL() *url.URL {
	return f.requestURL
}

func generateID() int64 {
	idMutex.Lock()
	defer idMutex.Unlock()

	id := idSeed
	idSeed++
	return id
}

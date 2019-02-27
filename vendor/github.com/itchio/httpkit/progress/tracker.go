package progress

import (
	"sync"
	"time"

	"github.com/itchio/httpkit/timeout"
)

var maxBucketDuration = 1 * time.Second

type Tracker struct {
	lastBandwidthUpdate time.Time
	lastBandwidthAlpha  float64
	bps                 float64
	bar                 *Bar
	alpha               float64
	lock                sync.Mutex
}

func NewTracker() *Tracker {
	bar := NewBar(100 * 100)
	bar.AlwaysUpdate = true
	bar.RefreshRate = 125 * time.Millisecond

	return &Tracker{
		// show to the 1/100ths of a percent (1/10000th of an alpha)
		bar: bar,
	}
}

func (c *Tracker) SetTotalBytes(totalBytes int64) {
	c.bar.TotalBytes = totalBytes
}

func (c *Tracker) SetSilent(silent bool) {
	c.bar.NotPrint = silent
}

func (c *Tracker) Start() {
	c.bar.Start()
}

func (c *Tracker) Finish() {
	c.bar.Postfix("")
	c.bar.Finish()
}

func (c *Tracker) Pause() {
	c.bar.AlwaysUpdate = false
}

func (c *Tracker) Resume() {
	c.bar.AlwaysUpdate = true
}

func (c *Tracker) SetProgress(alpha float64) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.bar.TotalBytes != 0 {
		if c.lastBandwidthUpdate.IsZero() {
			c.lastBandwidthUpdate = time.Now()
			c.lastBandwidthAlpha = alpha
		}
		bucketDuration := time.Since(c.lastBandwidthUpdate)

		if bucketDuration > maxBucketDuration {
			bytesSinceLastUpdate := float64(c.bar.TotalBytes) * (alpha - c.lastBandwidthAlpha)
			c.bps = bytesSinceLastUpdate / bucketDuration.Seconds()
			c.lastBandwidthUpdate = time.Now()
			c.lastBandwidthAlpha = alpha
		}
		// otherwise, keep current bps value
	} else {
		c.bps = 0
	}

	c.alpha = alpha
	c.bar.Set64(alphaToValue(alpha))
}

func (c *Tracker) Progress() float64 {
	return c.alpha
}

func (c *Tracker) ETA() time.Duration {
	return c.bar.GetTimeLeft()
}

func (c *Tracker) BPS() float64 {
	return timeout.GetBPS()
}

func (c *Tracker) WorkBPS() float64 {
	return c.bps
}

func (c *Tracker) Bar() *Bar {
	return c.bar
}

func alphaToValue(alpha float64) int64 {
	return int64(alpha * 100.0 * 100.0)
}

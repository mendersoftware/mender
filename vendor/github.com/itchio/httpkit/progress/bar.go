// Simple console progress bars
package progress

import (
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	// Default refresh rate - 200ms
	DEFAULT_REFRESH_RATE = time.Millisecond * 200
	FORMAT               = "[=>-]"
)

// Create new progress bar object using int64 as total
func NewBar(total int64) *Bar {
	pb := &Bar{
		Total:         total,
		RefreshRate:   DEFAULT_REFRESH_RATE,
		ShowPercent:   true,
		ShowCounters:  true,
		ShowBar:       true,
		ShowTimeLeft:  true,
		ShowFinalTime: true,
		Units:         U_NO,
		ManualUpdate:  false,

		finish:       make(chan struct{}),
		currentValue: -1,
		scale:        1.0,
		ewma:         &EWMA{},
		mu:           new(sync.Mutex),
	}
	return pb.Format(FORMAT)
}

// Create new object and start
func StartNewBar(total int64) *Bar {
	return NewBar(total).Start()
}

// Callback for custom output
// For example:
// bar.Callback = func(s string) {
//     mySuperPrint(s)
// }
//
type Callback func(out string)

type Bar struct {
	current int64 // current must be first member of struct (https://code.google.com/p/go/issues/detail?id=5278)

	Total                            int64
	RefreshRate                      time.Duration
	ShowPercent, ShowCounters        bool
	ShowSpeed, ShowTimeLeft, ShowBar bool
	ShowFinalTime                    bool
	Output                           io.Writer
	Callback                         Callback
	NotPrint                         bool
	Units                            Units
	Width                            int
	ForceWidth                       bool
	ManualUpdate                     bool

	TimeLeft   time.Duration
	TotalBytes int64

	// default width for unit numbers and time box
	UnitsWidth   int
	TimeBoxWidth int
	BarWidth     int

	finishOnce sync.Once // Guards isFinish
	finish     chan struct{}
	isFinish   bool

	startTime    time.Time
	startValue   int64
	currentValue int64
	scale        float64

	prefix, postfix string

	mu        *sync.Mutex
	ewma      *EWMA
	lastPrint string

	BarStart string
	BarEnd   string
	Empty    string
	Current  string
	CurrentN string

	AlwaysUpdate bool
}

// Start print
func (pb *Bar) Start() *Bar {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	pb.startTime = time.Now()
	pb.startValue = pb.current
	if pb.Total == 0 {
		pb.ShowTimeLeft = false
		pb.ShowPercent = false
	}
	if !pb.ManualUpdate {
		go pb.writer()
	}
	return pb
}

// Set64 sets the current value as int64
func (pb *Bar) Set64(current int64) *Bar {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	pb.current = current
	return pb
}

func (pb *Bar) SetScale(scale float64) *Bar {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	pb.scale = scale
	return pb
}

// Set prefix string
func (pb *Bar) Prefix(prefix string) *Bar {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	pb.prefix = prefix
	return pb
}

// Set postfix string
func (pb *Bar) Postfix(postfix string) *Bar {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	pb.postfix = postfix
	return pb
}

// Set custom format for bar
// Example: bar.Format("[=>_]")
// Example: bar.Format("[\x00=\x00>\x00-\x00]") // \x00 is the delimiter
func (pb *Bar) Format(format string) *Bar {
	var formatEntries []string
	if len(format) == 5 {
		formatEntries = strings.Split(format, "")
	} else {
		formatEntries = strings.Split(format, "\x00")
	}
	if len(formatEntries) == 5 {
		pb.BarStart = formatEntries[0]
		pb.BarEnd = formatEntries[4]
		pb.Empty = formatEntries[3]
		pb.Current = formatEntries[1]
		pb.CurrentN = formatEntries[2]
	}
	return pb
}

// Set bar refresh rate
func (pb *Bar) SetRefreshRate(rate time.Duration) *Bar {
	pb.RefreshRate = rate
	return pb
}

// Set units
// bar.SetUnits(U_NO) - by default
// bar.SetUnits(U_BYTES) - for Mb, Kb, etc
func (pb *Bar) SetUnits(units Units) *Bar {
	pb.Units = units
	return pb
}

// Set max width, if width is bigger than terminal width, will be ignored
func (pb *Bar) SetMaxWidth(width int) *Bar {
	pb.Width = width
	pb.ForceWidth = false
	return pb
}

// Set bar width
func (pb *Bar) SetWidth(width int) *Bar {
	pb.Width = width
	pb.ForceWidth = true
	return pb
}

// End print
func (pb *Bar) Finish() {
	// Protect multiple calls
	pb.finishOnce.Do(func() {
		pb.mu.Lock()
		defer pb.mu.Unlock()

		close(pb.finish)
		pb.write(atomic.LoadInt64(&pb.current))
		if !pb.NotPrint {
			fmt.Printf("\r%s\r", strings.Repeat(" ", pb.Width))
		}
		pb.isFinish = true
	})
}

func (pb *Bar) write(current int64) {
	width := pb.GetWidth() - 1

	var percentBox, countersBox, timeLeftBox, speedBox, barBox, end, out string

	// percents
	if pb.ShowPercent {
		var percent float64
		if pb.Total > 0 {
			percent = float64(current) / (float64(pb.Total) / float64(100))
		} else {
			percent = float64(current) / float64(100)
		}
		percentBox = fmt.Sprintf(" %6.02f%% ", percent)
	} else {
		percentBox = " "
	}

	// counters
	if pb.ShowCounters {
		if pb.Total > 0 {
			countersBox = fmt.Sprintf("%s / %s ", Format(current, pb.Units, pb.UnitsWidth), Format(pb.Total, pb.Units, pb.UnitsWidth))
		} else {
			countersBox = Format(current, pb.Units, pb.UnitsWidth) + " / ? "
		}
	}

	// time left
	fromStart := time.Now().Sub(pb.startTime)
	currentFromStart := current - pb.startValue
	select {
	case <-pb.finish:
		if pb.ShowFinalTime {
			var left time.Duration
			if pb.Total > 0 {
				left = (fromStart / time.Second) * time.Second
			} else {
				left = (time.Duration(currentFromStart) / time.Second) * time.Second
			}
			pb.ewma.Add(left.Seconds())
			timeLeftBox = (time.Duration(pb.ewma.Value()) * time.Second).String()
		}
	default:
		if pb.ShowTimeLeft && currentFromStart > 0 {
			perEntry := fromStart / time.Duration(currentFromStart)
			var left time.Duration
			if pb.Total > 0 {
				left = time.Duration(pb.Total-currentFromStart) * perEntry
				left = (left / time.Second) * time.Second
			} else {
				left = time.Duration(currentFromStart) * perEntry
				left = (left / time.Second) * time.Second
			}
			pb.TimeLeft = left
			timeLeftBox = FormatDuration(left)
		}
	}

	if len(timeLeftBox) < pb.TimeBoxWidth {
		timeLeftBox = fmt.Sprintf("%s%s", strings.Repeat(" ", pb.TimeBoxWidth-len(timeLeftBox)), timeLeftBox)
	}

	// speed
	if pb.ShowSpeed && currentFromStart > 0 {
		fromStart := time.Now().Sub(pb.startTime)
		speed := float64(currentFromStart) / (float64(fromStart) / float64(time.Second))
		speedBox = Format(int64(speed), pb.Units, pb.UnitsWidth) + "/s "
	}

	barWidth := escapeAwareRuneCountInString(countersBox + pb.BarStart + pb.BarEnd + percentBox + timeLeftBox + speedBox + pb.prefix + pb.postfix)
	// bar
	if pb.ShowBar {
		fullSize := min(pb.BarWidth, width-barWidth)
		size := int(math.Ceil(float64(fullSize) * pb.scale))
		padSize := fullSize - size
		if size > 0 {
			if pb.Total > 0 {
				curCount := int(math.Ceil((float64(current) / float64(pb.Total)) * float64(size)))
				emptCount := size - curCount
				barBox = pb.BarStart
				if emptCount < 0 {
					emptCount = 0
				}
				if curCount > size {
					curCount = size
				}
				if emptCount <= 0 {
					barBox += strings.Repeat(pb.Current, curCount)
				} else if curCount > 0 {
					barBox += strings.Repeat(pb.Current, curCount-1) + pb.CurrentN
				}
				barBox += strings.Repeat(pb.Empty, emptCount)
			} else {
				barBox = pb.BarStart
				pos := size - int(current)%int(size)
				if pos-1 > 0 {
					barBox += strings.Repeat(pb.Empty, pos-1)
				}
				barBox += pb.Current
				if size-pos-1 > 0 {
					barBox += strings.Repeat(pb.Empty, size-pos-1)
				}
			}
			if padSize > 0 {
				barBox += strings.Repeat(" ", padSize-1)
			}
			barBox += pb.BarEnd
		} else if padSize > 0 {
			barBox += pb.BarStart + strings.Repeat(" ", padSize-1) + pb.BarEnd
		}
	}

	// check len
	out = pb.prefix + countersBox + barBox + percentBox + speedBox + timeLeftBox + pb.postfix
	if escapeAwareRuneCountInString(out) < width {
		end = strings.Repeat(" ", width-utf8.RuneCountInString(out))
	}

	// and print!
	pb.lastPrint = out + end
	switch {
	case pb.Output != nil:
		fmt.Fprint(pb.Output, "\r"+out+end)
	case pb.Callback != nil:
		pb.Callback(out + end)
	case !pb.NotPrint:
		fmt.Print("\r" + out + end)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (pb *Bar) GetWidth() int {
	if pb.ForceWidth {
		return pb.Width
	}

	width := pb.Width
	return width
}

// Write the current state of the progressbar
func (pb *Bar) Update() {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	c := atomic.LoadInt64(&pb.current)
	if pb.AlwaysUpdate || c != pb.currentValue {
		pb.write(c)
		pb.currentValue = c
	}
}

func (pb *Bar) GOString() string {
	return pb.lastPrint
}

// Internal loop for writing progressbar
func (pb *Bar) writer() {
	pb.Update()
	for {
		select {
		case <-pb.finish:
			return
		case <-time.After(pb.RefreshRate):
			pb.Update()
		}
	}
}

func (pb *Bar) CurrentValue() int64 {
	return pb.currentValue
}

func (pb *Bar) GetTimeLeft() time.Duration {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	return pb.TimeLeft
}

type window struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

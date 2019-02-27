package retrycontext

import (
	"math"
	"math/rand"
	"time"

	"github.com/itchio/httpkit/neterr"

	"github.com/itchio/wharf/state"
)

// Context stores state related to an operation that should
// be retried.
type Context struct {
	Settings Settings

	Tries     int
	LastError error
}

// Settings configures a retry context, allowing to specify
// a maximum number of tries, whether to sleep or not, and
// an optional consumer to log activity to.
type Settings struct {
	MaxTries  int
	Consumer  *state.Consumer
	NoSleep   bool
	FakeSleep func(d time.Duration)
}

// New returns a new retry context with specific settings.
func New(settings Settings) *Context {
	return &Context{
		Tries:    0,
		Settings: settings,
	}
}

// NewDefault returns a new retry context with default settings.
func NewDefault() *Context {
	return New(Settings{
		MaxTries: 10,
	})
}

// ShouldTry must be used in a loop, like so:
//
// ----------------------------------------
// for rc.ShouldRetry() {
//	 err := someOperation()
//	 if err != nil {
//		 if isRetriable(err) {
//			 rc.Retry(err.Error())
//			 continue
//		 }
//	 }
//
//	 // succeeded!
//	 return nil // escape from loop
// }
//
// // THIS IS IMPORTANT
// return errors.New("task: too many failures, giving up")
// ----------------------------------------
//
// If you forget to return an error after the loop,
// if there are too many errors you'll just keep running.
func (rc *Context) ShouldTry() bool {
	return rc.Tries < rc.Settings.MaxTries
}

// Retry records an error that was retried (accessible in LastError)
// If a consumer was passed, it'll pause progress, and log the error.
// It's also in charge of sleeping (following exponential backoff)
func (rc *Context) Retry(err error) {
	rc.LastError = err

	if rc.Settings.Consumer != nil {
		rc.Settings.Consumer.PauseProgress()
		if neterr.IsNetworkError(err) {
			rc.Settings.Consumer.Infof("having network troubles...")
		} else {
			rc.Settings.Consumer.Infof("%v", err)
		}
	}

	// exponential backoff: 1, 2, 4, 8 seconds...
	delay := int(math.Pow(2, float64(rc.Tries)))
	// ...plus a random number of milliseconds.
	// see https://cloud.google.com/storage/docs/exponential-backoff
	jitter := rand.Int() % 1000

	if rc.Settings.Consumer != nil {
		rc.Settings.Consumer.Infof("Sleeping %d seconds then retrying", delay)
	}

	sleepDuration := time.Second*time.Duration(delay) + time.Millisecond*time.Duration(jitter)
	if rc.Settings.NoSleep {
		if rc.Settings.FakeSleep != nil {
			rc.Settings.FakeSleep(sleepDuration)
		}
	} else {
		time.Sleep(sleepDuration)
	}

	rc.Tries++

	if rc.Settings.Consumer != nil {
		rc.Settings.Consumer.ResumeProgress()
	}
}

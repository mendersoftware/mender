iothrottler
===========

A Go package for throttling application IO (such as bandwidth).

Installation
------------

If you have the Go Language installed type
`go get "github.com/efarrer/iothrottler"`

Example
-------
```go
import (
	"bytes"
	"fmt"
	"io"
	"github.com/efarrer/iothrottler"
	"os"
)

// Basic usage of a IOThrottlerPool to throttle reading from a file 
func ExampleIOThrottlerPool() {
	// Construct a bandwidth throttling pool that's limited to 100 bytes per second
	pool := iothrottler.NewIOThrottlerPool(iothrottler.BytesPerSecond * 100)
	defer pool.ReleasePool()

	file, err := os.Open("/dev/zero")
	if err != nil {
		// handle error
		return
	}
	defer file.Close()

	throttledFile, err := pool.AddReader(file)
	if err != nil {
		// handle error
		return
	}

	var zeros bytes.Buffer

	copied, err := io.CopyN(&zeros, throttledFile, 200)
	if err != nil {
		// handle error
	}

	fmt.Printf("Copied %v bytes\n", copied)
	// Output: Copied 200 bytes
}
```

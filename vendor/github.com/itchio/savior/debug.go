package savior

import (
	"log"
	"os"
)

var outputDebug = os.Getenv("SAVIOR_DEBUG") == "1"

// Debugf prints a message if the environment variable SAVIOR_DEBUG is set to "1"
func Debugf(format string, args ...interface{}) {
	if outputDebug {
		log.Printf(format, args...)
	}
}

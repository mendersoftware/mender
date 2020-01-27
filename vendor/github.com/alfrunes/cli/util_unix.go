// +build aix darwin dragonfly freebsd linux netbsd openbsd solaris !windows

package cli

import "golang.org/x/sys/unix"

// NewLine is OS specific.
const NewLine = "\n"

func getTerminalSize(fd int) (widthHeight [2]uint16, err error) {
	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil {
		return [2]uint16{0, 0}, err
	}
	return [2]uint16{ws.Col, ws.Row}, nil
}

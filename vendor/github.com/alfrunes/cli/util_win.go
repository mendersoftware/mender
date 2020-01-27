// +build windows

package cli

import "golang.org/x/sys/windows"

// NewLine is OS specific.
const NewLine = "\r\n"

func getTerminalSize(fd int) (widthHeight [2]uint16, err error) {
	var consoleInfo windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(
		windows.Handle(fd),
		&consoleInfo); err != nil {
		return [2]uint16{0, 0}, err
	}
	return [2]uint16{
		uint16(consoleInfo.Window.Left - consoleInfo.Window.Right + 1),
		uint16(consoleInfo.Window.Bottom - consoleInfo.Window.Top + 1),
	}, nil
}

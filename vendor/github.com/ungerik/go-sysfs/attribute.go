package sysfs

import (
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"syscall"
)

type Attribute struct {
	Path string
	File *os.File
}

func (attrib *Attribute) Exists() bool {
	return fileExists(attrib.Path)
}

func (attrib *Attribute) Open() (err error) {
	attrib.File, err = os.OpenFile(attrib.Path, os.O_RDWR|syscall.O_NONBLOCK, 0660)
	return err
}

func (attrib *Attribute) OpenRO() (err error) {
	attrib.File, err = os.OpenFile(attrib.Path, os.O_RDONLY|syscall.O_NONBLOCK, 0666)
	return err
}

func (attrib *Attribute) Close() (err error) {
	err = attrib.File.Close()
	attrib.File = nil
	return err
}

func (attrib *Attribute) Ioctl(request, arg uintptr) (result uintptr, errno syscall.Errno, err error) {
	if attrib.File == nil {
		err = attrib.Open()
		if err != nil {
			return
		}
		defer func() {
			e := attrib.Close()
			if err == nil {
				err = e
			}
		}()
	}
	result, _, errno = syscall.Syscall(syscall.SYS_IOCTL, attrib.File.Fd(), request, arg)
	return result, errno, err
}

func (attrib *Attribute) Read() (str string, err error) {
	if attrib.File == nil {
		err = attrib.OpenRO()
		if err != nil {
			return
		}
		defer func() {
			e := attrib.Close()
			if err == nil {
				err = e
			}
		}()
	}
	attrib.File.Seek(0, os.SEEK_SET)
	data, err := ioutil.ReadAll(attrib.File)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (attrib *Attribute) Write(value string) (err error) {
	if attrib.File == nil {
		err = attrib.Open()
		if err != nil {
			return
		}
		defer func() {
			e := attrib.Close()
			if err == nil {
				err = e
			}
		}()
	}
	attrib.File.Seek(0, os.SEEK_SET)
	_, err = attrib.File.WriteString(value)
	return err
}

func (attrib *Attribute) Print(value interface{}) (err error) {
	if attrib.File == nil {
		err = attrib.Open()
		if err != nil {
			return
		}
		defer func() {
			e := attrib.Close()
			if err == nil {
				err = e
			}
		}()
	}
	attrib.File.Seek(0, os.SEEK_SET)
	_, err = fmt.Fprint(attrib.File, value)
	return err
}

func (attrib *Attribute) Scan(value interface{}) (err error) {
	if attrib.File == nil {
		err = attrib.Open()
		if err != nil {
			return
		}
		defer func() {
			e := attrib.Close()
			if err == nil {
				err = e
			}
		}()
	}
	attrib.File.Seek(0, os.SEEK_SET)
	_, err = fmt.Fscan(attrib.File, value)
	return err
}

func (attrib *Attribute) Printf(format string, args ...interface{}) (err error) {
	if attrib.File == nil {
		err = attrib.Open()
		if err != nil {
			return
		}
		defer func() {
			e := attrib.Close()
			if err == nil {
				err = e
			}
		}()
	}
	attrib.File.Seek(0, os.SEEK_SET)
	_, err = fmt.Fprintf(attrib.File, format, args...)
	return err
}

func (attrib *Attribute) Scanf(format string, args ...interface{}) (err error) {
	if attrib.File == nil {
		err = attrib.Open()
		if err != nil {
			return
		}
		defer func() {
			e := attrib.Close()
			if err == nil {
				err = e
			}
		}()
	}
	attrib.File.Seek(0, os.SEEK_SET)
	_, err = fmt.Fscanf(attrib.File, format, args...)
	return err
}

func (attrib *Attribute) ReadBytes() (data []byte, err error) {
	if attrib.File == nil {
		err = attrib.Open()
		if err != nil {
			return
		}
		defer func() {
			e := attrib.Close()
			if err == nil {
				err = e
			}
		}()
	}
	attrib.File.Seek(0, os.SEEK_SET)
	return ioutil.ReadAll(attrib.File)
}

func (attrib *Attribute) WriteBytes(data []byte) (err error) {
	if attrib.File == nil {
		err = attrib.Open()
		if err != nil {
			return
		}
		defer func() {
			e := attrib.Close()
			if err == nil {
				err = e
			}
		}()
	}
	_, err = attrib.File.WriteAt(data, 0)
	return err
}

func (attrib *Attribute) ReadByte() (value byte, err error) {
	if attrib.File == nil {
		err = attrib.Open()
		if err != nil {
			return
		}
		defer func() {
			e := attrib.Close()
			if err == nil {
				err = e
			}
		}()
	}
	data := make([]byte, 1)
	_, err = attrib.File.ReadAt(data, 0)
	return data[0], err
}

func (attrib *Attribute) WriteByte(value byte) (err error) {
	return attrib.WriteBytes([]byte{value})
}

func (attrib *Attribute) ReadInt() (value int, err error) {
	s, err := attrib.Read()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}

func (attrib *Attribute) ReadUint64() (value uint64, err error) {
	s, err := attrib.Read()
	if err != nil {
		return 0, err
	}

	s = strings.TrimSpace(s)

	return strconv.ParseUint(s, 10, 64)
}

func (attrib *Attribute) WriteInt(value int) (err error) {
	return attrib.Write(strconv.Itoa(value))
}

func (attrib *Attribute) WriteUint64(value uint64) (err error) {
	return attrib.Write(strconv.FormatUint(value, 10))
}

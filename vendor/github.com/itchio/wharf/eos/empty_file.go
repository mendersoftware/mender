package eos

import (
	"fmt"
	"io"
	"os"
	"time"
)

type emptyFile struct{}

var _ File = (*emptyFile)(nil)

func (ef *emptyFile) Close() error {
	return nil
}

func (ef *emptyFile) Seek(offset int64, mode int) (int64, error) {
	if offset != 0 {
		return 0, fmt.Errorf("cannot seek into /dev/null")
	}
	return 0, nil
}

func (ef *emptyFile) Read(buf []byte) (int, error) {
	return 0, io.EOF
}

func (ef *emptyFile) ReadAt(buf []byte, offset int64) (int, error) {
	return 0, io.EOF
}

func (ef *emptyFile) Stat() (os.FileInfo, error) {
	return &nullStats{}, nil
}

type nullStats struct{}

var _ os.FileInfo = (*nullStats)(nil)

func (ns *nullStats) IsDir() bool {
	return false
}

func (ns *nullStats) Size() int64 {
	return 0
}

func (ns *nullStats) ModTime() time.Time {
	return time.Time{}
}

func (ns *nullStats) Mode() os.FileMode {
	return os.FileMode(0644)
}

func (ns *nullStats) Name() string {
	return "/dev/null"
}

func (ns *nullStats) Sys() interface{} {
	return nil
}

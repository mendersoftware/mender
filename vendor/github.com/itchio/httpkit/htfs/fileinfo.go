package htfs

import (
	"os"
	"time"
)

// FileInfo implements os.FileInfo for Files
type FileInfo struct {
	file *File
}

var _ os.FileInfo = (*FileInfo)(nil)

func (hfi *FileInfo) Name() string {
	return hfi.file.name
}

func (hfi *FileInfo) Size() int64 {
	return hfi.file.size
}

func (hfi *FileInfo) Mode() os.FileMode {
	return os.FileMode(0)
}

func (hfi *FileInfo) ModTime() time.Time {
	return time.Now()
}

func (hfi *FileInfo) IsDir() bool {
	return false
}

func (hfi *FileInfo) Sys() interface{} {
	return nil
}

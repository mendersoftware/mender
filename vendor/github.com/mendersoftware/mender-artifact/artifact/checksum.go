// Copyright 2020 Northern.tech AS
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//        http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

package artifact

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/minio/sha256-simd"
	"github.com/pkg/errors"
)

type Checksum struct {
	w io.Writer // underlying writer
	h hash.Hash // writer calculated hash

	r io.Reader
	c []byte // reader pre-loaded checksum
}

func NewWriterChecksum(w io.Writer) *Checksum {
	if w == nil {
		return new(Checksum)
	}

	h := sha256.New()
	return &Checksum{
		w: io.MultiWriter(h, w),
		h: h,
	}
}

func NewReaderChecksum(r io.Reader, sum []byte) *Checksum {
	if r == nil {
		return new(Checksum)
	}

	h := sha256.New()
	return &Checksum{
		r: io.TeeReader(r, h),
		c: sum,
		h: h,
	}
}

func (c *Checksum) Write(p []byte) (int, error) {
	if c.w == nil {
		return 0, syscall.EBADF
	}
	return c.w.Write(p)
}

// Do not call Read directly; use io.Copy instead as we are
// calculating checksum only after receiving io.EOF.
func (c *Checksum) Read(p []byte) (int, error) {
	if c.r == nil {
		return 0, syscall.EBADF
	}
	n, err := c.r.Read(p)
	if err == io.EOF {
		// verify checksum
		if verErr := c.Verify(); verErr != nil {
			return 0, verErr
		}
	}
	return n, err
}

func (c *Checksum) Checksum() []byte {
	if c.h == nil {
		return nil
	}
	sum := c.h.Sum(nil)
	checksum := make([]byte, hex.EncodedLen(len(sum)))
	hex.Encode(checksum, sum)
	return checksum
}

func (c *Checksum) Verify() error {
	sum := c.Checksum()
	if !bytes.Equal(c.c, sum) {
		return errors.Errorf("invalid checksum; expected: [%s]; actual: [%s]",
			c.c, sum)
	}
	return nil
}

type ChecksumStore struct {
	// raw is storing raw data that is read from manifest file;
	// we need to keep raw data as iterating over sums map may produce
	// different result each time map is traversed
	raw *bytes.Buffer
	// sums is a map of all files and its checksums;
	// key is the name of the file and value is the checksum
	sums map[string]([]byte)
	// A map which contains the same keys as sums, used to mark each file,
	// and then check at the end that all files have been visited.
	marked map[string]bool
}

func NewChecksumStore() *ChecksumStore {
	return &ChecksumStore{
		sums:   make(map[string]([]byte), 1),
		raw:    bytes.NewBuffer(nil),
		marked: make(map[string]bool, 1),
	}
}

func (c *ChecksumStore) Add(file string, sum []byte) error {
	if _, ok := c.sums[file]; ok {
		return os.ErrExist
	}

	c.sums[file] = sum
	c.marked[file] = false
	_, err := c.raw.WriteString(fmt.Sprintf("%s  %s\n", sum, file))
	return err
}

func (c *ChecksumStore) Get(file string) ([]byte, error) {
	sum, ok := c.sums[file]
	if !ok {
		return nil, errors.Errorf("checksum: checksum missing for file: '%s'", file)
	}
	return sum, nil
}

// Same as Get(), but also marks the file as visited.
func (c *ChecksumStore) GetAndMark(file string) ([]byte, error) {
	sum, err := c.Get(file)
	if err == nil {
		c.marked[file] = true
	}
	return sum, err
}

func (c *ChecksumStore) FilesNotMarked() []string {
	var list []string
	for file, marked := range c.marked {
		if !marked {
			list = append(list, file)
		}
	}
	return list
}

func (c *ChecksumStore) GetRaw() []byte {
	return c.raw.Bytes()
}

func (c *ChecksumStore) ReadRaw(data []byte) error {
	raw := bytes.NewBuffer(data)
	for {
		line, err := raw.ReadString('\n')
		if err == io.EOF {
			break
		} else if err != nil {
			return errors.Wrap(err, "checksum: can not read raw")
		}
		if err = c.readChecksums(line); err != nil {
			return err
		}
	}
	return nil
}

func (c *ChecksumStore) readChecksums(line string) error {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil
	}
	chunks := strings.Split(trimmed, "  ")
	if len(chunks) != 2 {
		return errors.Errorf("checksum: malformed checksum line: '%s'", line)
	}
	// add element to map
	return c.Add(chunks[1], []byte(chunks[0]))
}

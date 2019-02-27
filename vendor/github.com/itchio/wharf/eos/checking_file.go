package eos

import (
	"bytes"
	"log"
	"os"

	"github.com/pkg/errors"
)

type CheckingFile struct {
	Reference File
	Trainee   File
}

var _ File = (*CheckingFile)(nil)

func (cf *CheckingFile) Close() error {
	err1 := cf.Reference.Close()
	err2 := cf.Trainee.Close()

	if err1 != nil {
		return err1
	}

	if err2 != nil {
		return err2
	}
	return nil
}

func (cf *CheckingFile) Read(tBuf []byte) (int, error) {
	rBuf := make([]byte, len(tBuf))
	rReadBytes, rErr := cf.Reference.Read(rBuf)

	tReadBytes, tErr := cf.Trainee.Read(tBuf)

	if rErr != nil {
		if tErr != nil {
			log.Printf("reference error: %s", rErr.Error())
			log.Printf("  trainee error: %s", tErr.Error())
			// cool, we'll return that at the end
		} else {
			return 0, errors.Errorf("reference had following error, trainee had no error: %+v", rErr)
		}
	} else {
		if tErr != nil {
			return 0, errors.Errorf("reference had no error, trainee had error: %+v", tErr)
		}
	}

	if rReadBytes != tReadBytes {
		return 0, errors.Errorf("reference read %d bytes, trainee read %d", rReadBytes, tReadBytes)
	}

	if !bytes.Equal(rBuf[:rReadBytes], tBuf[:rReadBytes]) {
		return 0, errors.Errorf("found difference in %d-bytes chunk read by reference & trainee", rReadBytes)
	}

	return tReadBytes, tErr
}

func (cf *CheckingFile) ReadAt(tBuf []byte, offset int64) (int, error) {
	rBuf := make([]byte, len(tBuf))
	rReadBytes, rErr := cf.Reference.ReadAt(rBuf, offset)

	tReadBytes, tErr := cf.Trainee.ReadAt(tBuf, offset)

	if rErr != nil {
		if tErr != nil {
			log.Printf("reference error: %s", rErr.Error())
			log.Printf("  trainee error: %s", tErr.Error())
			// cool, we'll return that later
		} else {
			return 0, errors.Errorf("reference had following error, trainee had no error: %+v", rErr)
		}
	} else {
		if tErr != nil {
			return 0, errors.Errorf("reference had no error, trainee had error: %+v", tErr)
		}
	}

	if rReadBytes != tReadBytes {
		return 0, errors.Errorf("reference read %d bytes, trainee read %d", rReadBytes, tReadBytes)
	}

	if !bytes.Equal(rBuf[:rReadBytes], tBuf[:rReadBytes]) {
		firstDifferent := rReadBytes
		for i := 0; i < rReadBytes; i++ {
			if rBuf[i] != tBuf[i] {
				firstDifferent = i
				break
			}
		}

		numDifferent := 0
		for i := 0; i < rReadBytes; i++ {
			if rBuf[i] != tBuf[i] {
				numDifferent++
			}
		}

		return 0, errors.Errorf("reference & trainee read different bytes at %d, first different is at %d / %d, %d bytes are different", offset, firstDifferent, rReadBytes, numDifferent)
	}

	return tReadBytes, tErr
}

func (cf *CheckingFile) Seek(offset int64, whence int) (int64, error) {
	rOffset, rErr := cf.Reference.Seek(offset, whence)
	tOffset, tErr := cf.Trainee.Seek(offset, whence)

	if rErr != nil {
		if tErr != nil {
			log.Printf("reference error: %s", rErr.Error())
			log.Printf("  trainee error: %s", tErr.Error())
			// cool, we'll return that at the end
		} else {
			return 0, errors.Errorf("reference had following error, trainee had no error: %+v", rErr)
		}
	} else {
		if tErr != nil {
			return 0, errors.Errorf("reference had no error, trainee had error: %+v", tErr)
		}
	}

	if rOffset != tOffset {
		return 0, errors.Errorf("reference seeked to %d, trainee seeked to %d", rOffset, tOffset)
	}

	return tOffset, tErr
}

func (cf *CheckingFile) Stat() (os.FileInfo, error) {
	_, rErr := cf.Reference.Stat()
	tStat, tErr := cf.Trainee.Stat()

	if rErr != nil {
		if tErr != nil {
			log.Printf("reference error: %s", rErr.Error())
			log.Printf("  trainee error: %s", tErr.Error())
			// cool, we'll return that at the end
		} else {
			return nil, errors.Errorf("reference had following error, trainee had no error: %+v", rErr)
		}
	} else {
		if tErr != nil {
			return nil, errors.Errorf("reference had no error, trainee had error: %+v", tErr)
		}
	}

	return tStat, tErr
}

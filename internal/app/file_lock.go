package app

import (
	"errors"
	"os"
	"syscall"
)

type localFileLock struct {
	file *os.File
}

func acquireLocalFileLock(path string) (*localFileLock, error) {
	lockFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile.Chmod(0o600); err != nil {
		lockFile.Close()
		return nil, err
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		lockFile.Close()
		return nil, err
	}

	return &localFileLock{file: lockFile}, nil
}

func (lock *localFileLock) Close() error {
	return errors.Join(
		syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN),
		lock.file.Close(),
	)
}

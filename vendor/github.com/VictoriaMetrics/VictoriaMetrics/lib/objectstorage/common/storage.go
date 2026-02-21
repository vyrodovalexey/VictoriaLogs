package common

import (
	"errors"
	"io"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/bytesutil"
)

type StorageClient interface {
	Close()
	CreateFile(path string, data []byte) error
	DeletePath(path string, recursive bool) error
	GetFileSize(path string) (uint64, error)
	GetPath(path string) string
	GetRoot() string
	IsFileExist(path string) (bool, error)
	IsTransientError(e error) bool
	ListFiles(path string) (map[string]uint64, error)
	ReadDir(path string) ([]string, error)
	ReadFile(path string, wr io.WriterAt) error
	ReadRange(path string, buf []byte, offset int64) (int, error)
	Sync(src, dest string) error
}

func GetWriteAtBuffer() *bytesutil.ByteBuffer {
	return bbPool.Get()
}

func PutWriteAtBuffer(bb *bytesutil.ByteBuffer) {
	bbPool.Put(bb)
}

var bbPool bytesutil.ByteBufferPool

func NewReadCloser(sc StorageClient, path string, size uint64) *ReadCloser {
	return &ReadCloser{
		sc:   sc,
		path: path,
		size: int64(size),
	}
}

type ReadCloser struct {
	sc     StorageClient
	path   string
	size   int64
	offset int64
}

func (rc *ReadCloser) Path() string {
	return rc.path
}

func (rc *ReadCloser) MustClose() {}

func (rc *ReadCloser) ReadAt(buf []byte, offset int64) error {
	_, err := rc.readAt(buf, offset)
	if err == io.EOF {
		err = nil
	}
	return err
}

func (rc *ReadCloser) readAt(buf []byte, offset int64) (int, error) {
	if offset < 0 {
		return 0, errors.New("negative offsets found")
	}
	if offset >= rc.size {
		return 0, io.EOF
	}
	want := rc.size - offset
	if int64(len(buf)) < want {
		want = int64(len(buf))
	}
	n, err := rc.sc.ReadRange(rc.path, buf[:want], offset)
	if err == nil && offset+int64(n) >= rc.size {
		err = io.EOF
	}
	return n, err
}

func (rc *ReadCloser) Read(buf []byte) (n int, err error) {
	if rc.offset >= rc.size {
		return 0, io.EOF
	}
	if max := rc.size - rc.offset; int64(len(buf)) > max {
		buf = buf[:max]
	}
	n, err = rc.readAt(buf, rc.offset)
	rc.offset += int64(n)
	return
}

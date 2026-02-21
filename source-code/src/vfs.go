package src

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type vfsHandler interface {
	ReadDir(dir string) ([]fs.DirEntry, error)
	Open(file string) (fs.File, error)
	Stat(file string) (fs.FileInfo, error)
	Chdir(dir string) error
	Getwd() (string, error)
}

type localVFS struct{}

func (l localVFS) ReadDir(dir string) ([]fs.DirEntry, error) { return os.ReadDir(dir) }
func (l localVFS) Open(file string) (fs.File, error)         { return os.Open(file) }
func (l localVFS) Stat(file string) (fs.FileInfo, error)     { return os.Stat(file) }
func (l localVFS) Chdir(dir string) error                    { return os.Chdir(dir) }
func (l localVFS) Getwd() (string, error)                    { return os.Getwd() }

type sftpVFS struct {
	client *sftp.Client
}

func newSFTPVFS(host, user, pass string) (*sftpVFS, error) {
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	conn, err := ssh.Dial("tcp", host, config)
	if err != nil {
		return nil, err
	}
	client, err := sftp.NewClient(conn)
	if err != nil {
		return nil, err
	}
	return &sftpVFS{client: client}, nil
}

func (s *sftpVFS) ReadDir(dir string) ([]fs.DirEntry, error) {
	files, err := s.client.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var entries []fs.DirEntry
	for _, f := range files {
		entries = append(entries, &sftpDirEntry{info: f})
	}
	return entries, nil
}

func (s *sftpVFS) Open(file string) (fs.File, error)     { return s.client.Open(file) }
func (s *sftpVFS) Stat(file string) (fs.FileInfo, error) { return s.client.Stat(file) }
func (s *sftpVFS) Chdir(dir string) error {
	_, err := s.client.Stat(dir)
	return err
}
func (s *sftpVFS) Getwd() (string, error) { return s.client.Getwd() }

type sftpDirEntry struct {
	info fs.FileInfo
}

func (e *sftpDirEntry) Name() string               { return e.info.Name() }
func (e *sftpDirEntry) IsDir() bool                { return e.info.IsDir() }
func (e *sftpDirEntry) Type() fs.FileMode          { return e.info.Mode() }
func (e *sftpDirEntry) Info() (fs.FileInfo, error) { return e.info, nil }

type tarVFS struct {
	filename string
	isGz     bool
	entries  map[string]*tar.Header
}

func newTarVFS(filename string) (*tarVFS, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	isGz := strings.HasSuffix(filename, ".gz")
	var gzr *gzip.Reader
	if isGz {
		gzr, err = gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		r = gzr
	}
	tr := tar.NewReader(r)
	entries := make(map[string]*tar.Header)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		entries[hdr.Name] = hdr
	}
	if isGz {
		gzr.Close()
	}
	return &tarVFS{filename: filename, isGz: isGz, entries: entries}, nil
}

func (t *tarVFS) ReadDir(dir string) ([]fs.DirEntry, error) {
	var entries []fs.DirEntry
	prefix := strings.TrimPrefix(dir, t.filename+"/") + "/"
	for name, hdr := range t.entries {
		if strings.HasPrefix(name, prefix) {
			suffix := name[len(prefix):]
			if strings.Contains(suffix, "/") {
				continue
			}
			entries = append(entries, &tarDirEntry{name: filepath.Base(name), hdr: hdr})
		}
	}
	return entries, nil
}

func (t *tarVFS) Open(path string) (fs.File, error) {
	f, err := os.Open(t.filename)
	if err != nil {
		return nil, err
	}
	var r io.Reader = f
	var closer io.Closer = f
	if t.isGz {
		gzr, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		r = gzr
		closer = gzr
	}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			closer.Close()
			return nil, fs.ErrNotExist
		}
		if err != nil {
			closer.Close()
			return nil, err
		}
		if hdr.Name == path {
			return &tarFile{reader: io.LimitReader(tr, hdr.Size), closer: closer, name: path, size: hdr.Size}, nil
		}
	}
}

func (t *tarVFS) Stat(path string) (fs.FileInfo, error) {
	hdr, ok := t.entries[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return hdr.FileInfo(), nil
}

func (t *tarVFS) Chdir(dir string) error {
	return nil
}

func (t *tarVFS) Getwd() (string, error) { return t.filename, nil }

type tarDirEntry struct {
	name string
	hdr  *tar.Header
}

func (e *tarDirEntry) Name() string               { return e.name }
func (e *tarDirEntry) IsDir() bool                { return e.hdr.Typeflag == tar.TypeDir }
func (e *tarDirEntry) Type() fs.FileMode          { return e.hdr.FileInfo().Mode() }
func (e *tarDirEntry) Info() (fs.FileInfo, error) { return e.hdr.FileInfo(), nil }

type tarFile struct {
	reader io.Reader
	closer io.Closer
	name   string
	size   int64
	pos    int64
}

func (tf *tarFile) Read(b []byte) (int, error) {
	n, err := tf.reader.Read(b)
	tf.pos += int64(n)
	return n, err
}

func (tf *tarFile) Close() error { return tf.closer.Close() }

func (tf *tarFile) Stat() (fs.FileInfo, error) {
	return &tarFileInfo{name: tf.name, size: tf.size}, nil
}

type tarFileInfo struct {
	name string
	size int64
}

func (i *tarFileInfo) Name() string       { return i.name }
func (i *tarFileInfo) Size() int64        { return i.size }
func (i *tarFileInfo) Mode() fs.FileMode  { return 0644 }
func (i *tarFileInfo) ModTime() time.Time { return time.Now() }
func (i *tarFileInfo) IsDir() bool        { return false }
func (i *tarFileInfo) Sys() any           { return nil }

type zipVFS struct {
	filename string
	reader   *zip.Reader
	file     *os.File
}

func newZipVFS(filename string) (*zipVFS, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	r, err := zip.NewReader(f, fi.Size())
	if err != nil {
		f.Close()
		return nil, err
	}
	return &zipVFS{filename: filename, reader: r, file: f}, nil
}

func (z *zipVFS) ReadDir(dir string) ([]fs.DirEntry, error) {
	var entries []fs.DirEntry
	prefix := strings.TrimPrefix(dir, z.filename+"/") + "/"
	seenDirs := make(map[string]bool)
	for _, f := range z.reader.File {
		name := f.Name
		if strings.HasPrefix(name, prefix) {
			suffix := name[len(prefix):]
			if suffix == "" {
				continue
			}
			if idx := strings.Index(suffix, "/"); idx != -1 {
				dirName := suffix[:idx]
				if seenDirs[dirName] {
					continue
				}
				seenDirs[dirName] = true
				entries = append(entries, &zipDirEntry{f: &zip.File{FileHeader: zip.FileHeader{Name: dirName}}, isDir: true})
			} else {
				entries = append(entries, &zipDirEntry{f: f, isDir: false})
			}
		}
	}
	return entries, nil
}

func (z *zipVFS) Open(path string) (fs.File, error) {
	for _, f := range z.reader.File {
		if f.Name == path {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			return &zipFile{reader: rc, name: path, size: int64(f.UncompressedSize64)}, nil
		}
	}
	return nil, fs.ErrNotExist
}

func (z *zipVFS) Stat(path string) (fs.FileInfo, error) {
	for _, f := range z.reader.File {
		if f.Name == path {
			return f.FileInfo(), nil
		}
	}
	return nil, fs.ErrNotExist
}

func (z *zipVFS) Chdir(dir string) error {
	return nil
}

func (z *zipVFS) Getwd() (string, error) { return z.filename, nil }

type zipDirEntry struct {
	f     *zip.File
	isDir bool
}

func (e *zipDirEntry) Name() string               { return e.f.Name }
func (e *zipDirEntry) IsDir() bool                { return e.isDir }
func (e *zipDirEntry) Type() fs.FileMode          { return e.f.Mode() }
func (e *zipDirEntry) Info() (fs.FileInfo, error) { return e.f.FileInfo(), nil }

type zipFile struct {
	reader io.ReadCloser
	name   string
	size   int64
}

func (zf *zipFile) Read(b []byte) (int, error) { return zf.reader.Read(b) }
func (zf *zipFile) Close() error               { return zf.reader.Close() }
func (zf *zipFile) Stat() (fs.FileInfo, error) { return &zipFileInfo{name: zf.name, size: zf.size}, nil }

type zipFileInfo struct {
	name string
	size int64
}

func (i *zipFileInfo) Name() string       { return i.name }
func (i *zipFileInfo) Size() int64        { return i.size }
func (i *zipFileInfo) Mode() fs.FileMode  { return 0644 }
func (i *zipFileInfo) ModTime() time.Time { return time.Now() }
func (i *zipFileInfo) IsDir() bool        { return false }
func (i *zipFileInfo) Sys() any           { return nil }

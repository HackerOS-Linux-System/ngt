package src

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// vfsHandler is the extended interface supporting read+write operations
type vfsHandler interface {
	ReadDir(dir string) ([]fs.DirEntry, error)
	Open(file string) (fs.File, error)
	Stat(file string) (fs.FileInfo, error)
	Chdir(dir string) error
	Getwd() (string, error)
	// Extended ops
	Remove(path string) error
	Rename(src, dst string) error
	MkdirAll(path string, perm fs.FileMode) error
	Create(path string) (io.WriteCloser, error)
	VFSName() string
}

// ─────────────────────────────────────────────
//  Local VFS
// ─────────────────────────────────────────────

type localVFS struct{}

func (l localVFS) ReadDir(dir string) ([]fs.DirEntry, error) { return os.ReadDir(dir) }
func (l localVFS) Open(file string) (fs.File, error)         { return os.Open(file) }
func (l localVFS) Stat(file string) (fs.FileInfo, error)     { return os.Stat(file) }
func (l localVFS) Chdir(dir string) error                    { return os.Chdir(dir) }
func (l localVFS) Getwd() (string, error)                    { return os.Getwd() }
func (l localVFS) Remove(path string) error                  { return os.RemoveAll(path) }
func (l localVFS) Rename(src, dst string) error              { return os.Rename(src, dst) }
func (l localVFS) MkdirAll(path string, perm fs.FileMode) error {
	return os.MkdirAll(path, perm)
}
func (l localVFS) Create(path string) (io.WriteCloser, error) { return os.Create(path) }
func (l localVFS) VFSName() string                            { return "local" }

// ─────────────────────────────────────────────
//  SFTP VFS
// ─────────────────────────────────────────────

type sftpVFS struct {
	client *sftp.Client
	host   string
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
	return &sftpVFS{client: client, host: host}, nil
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
func (s *sftpVFS) Chdir(dir string) error                { _, err := s.client.Stat(dir); return err }
func (s *sftpVFS) Getwd() (string, error)                { return s.client.Getwd() }
func (s *sftpVFS) Remove(path string) error              { return s.client.RemoveAll(path) }
func (s *sftpVFS) Rename(src, dst string) error          { return s.client.Rename(src, dst) }
func (s *sftpVFS) MkdirAll(path string, perm fs.FileMode) error {
	return s.client.MkdirAll(path)
}
func (s *sftpVFS) Create(path string) (io.WriteCloser, error) { return s.client.Create(path) }
func (s *sftpVFS) VFSName() string                            { return "sftp://" + s.host }

type sftpDirEntry struct{ info fs.FileInfo }

func (e *sftpDirEntry) Name() string               { return e.info.Name() }
func (e *sftpDirEntry) IsDir() bool                { return e.info.IsDir() }
func (e *sftpDirEntry) Type() fs.FileMode          { return e.info.Mode() }
func (e *sftpDirEntry) Info() (fs.FileInfo, error) { return e.info, nil }

// ─────────────────────────────────────────────
//  Podman VFS
// ─────────────────────────────────────────────

type podmanVFS struct {
	containerID   string
	containerName string
	cwd           string
}

// podmanContainerInfo holds JSON response from podman inspect
type podmanContainerInfo struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	State struct {
		Status string `json:"Status"`
	} `json:"State"`
}

func newPodmanVFS(containerID string) (*podmanVFS, error) {
	// Check podman is available
	if _, err := exec.LookPath("podman"); err != nil {
		return nil, fmt.Errorf("podman not found in PATH: %w", err)
	}
	// Inspect container
	out, err := exec.Command("podman", "inspect", "--format", "json", containerID).Output()
	if err != nil {
		return nil, fmt.Errorf("container not found: %w", err)
	}
	var infos []podmanContainerInfo
	if err := json.Unmarshal(out, &infos); err != nil || len(infos) == 0 {
		return nil, fmt.Errorf("failed to parse container info")
	}
	info := infos[0]
	name := strings.TrimPrefix(info.Name, "/")
	return &podmanVFS{
		containerID:   info.ID[:12],
		containerName: name,
		cwd:           "/",
	}, nil
}

// podmanExec runs a command inside the container and returns stdout
func (p *podmanVFS) podmanExec(args ...string) ([]byte, error) {
	fullArgs := append([]string{"exec", p.containerID}, args...)
	return exec.Command("podman", fullArgs...).Output()
}

func (p *podmanVFS) ReadDir(dir string) ([]fs.DirEntry, error) {
	out, err := p.podmanExec("ls", "-la", "--time-style=long-iso", dir)
	if err != nil {
		return nil, fmt.Errorf("ls failed in container: %w", err)
	}
	var entries []fs.DirEntry
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "total") {
			continue
		}
		entry := parsePodmanLsLine(line)
		if entry == nil || entry.Name() == "." || entry.Name() == ".." {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func parsePodmanLsLine(line string) *podmanDirEntry {
	// format: permissions links owner group size date time name
	fields := strings.Fields(line)
	if len(fields) < 9 {
		return nil
	}
	perms := fields[0]
	isDir := strings.HasPrefix(perms, "d")
	name := strings.Join(fields[8:], " ")
	// size
	var size int64
	fmt.Sscanf(fields[4], "%d", &size)
	return &podmanDirEntry{
		name:  name,
		isDir: isDir,
		size:  size,
	}
}

type podmanDirEntry struct {
	name  string
	isDir bool
	size  int64
}

func (e *podmanDirEntry) Name() string { return e.name }
func (e *podmanDirEntry) IsDir() bool  { return e.isDir }
func (e *podmanDirEntry) Type() fs.FileMode {
	if e.isDir {
		return fs.ModeDir
	}
	return 0
}
func (e *podmanDirEntry) Info() (fs.FileInfo, error) {
	return &podmanFileInfo{name: e.name, isDir: e.isDir, size: e.size}, nil
}

type podmanFileInfo struct {
	name  string
	isDir bool
	size  int64
}

func (i *podmanFileInfo) Name() string       { return i.name }
func (i *podmanFileInfo) Size() int64        { return i.size }
func (i *podmanFileInfo) Mode() fs.FileMode  { return 0644 }
func (i *podmanFileInfo) ModTime() time.Time { return time.Now() }
func (i *podmanFileInfo) IsDir() bool        { return i.isDir }
func (i *podmanFileInfo) Sys() any           { return nil }

func (p *podmanVFS) Open(file string) (fs.File, error) {
	out, err := exec.Command("podman", "exec", p.containerID, "cat", file).Output()
	if err != nil {
		return nil, fmt.Errorf("cannot read file from container: %w", err)
	}
	return &podmanFile{
		name:    filepath.Base(file),
		content: out,
		size:    int64(len(out)),
	}, nil
}

type podmanFile struct {
	name    string
	content []byte
	size    int64
	offset  int
}

func (f *podmanFile) Read(b []byte) (int, error) {
	if f.offset >= len(f.content) {
		return 0, io.EOF
	}
	n := copy(b, f.content[f.offset:])
	f.offset += n
	return n, nil
}
func (f *podmanFile) Close() error { return nil }
func (f *podmanFile) Stat() (fs.FileInfo, error) {
	return &podmanFileInfo{name: f.name, size: f.size}, nil
}

func (p *podmanVFS) Stat(file string) (fs.FileInfo, error) {
	out, err := p.podmanExec("stat", "-c", "%n %s %F", file)
	if err != nil {
		return nil, fs.ErrNotExist
	}
	fields := strings.Fields(string(out))
	if len(fields) < 3 {
		return nil, fmt.Errorf("unexpected stat output")
	}
	var size int64
	fmt.Sscanf(fields[1], "%d", &size)
	isDir := strings.Contains(fields[2], "directory")
	return &podmanFileInfo{name: filepath.Base(file), isDir: isDir, size: size}, nil
}

func (p *podmanVFS) Chdir(dir string) error {
	_, err := p.podmanExec("test", "-d", dir)
	if err != nil {
		return fmt.Errorf("directory does not exist in container: %s", dir)
	}
	p.cwd = dir
	return nil
}

func (p *podmanVFS) Getwd() (string, error) { return p.cwd, nil }

func (p *podmanVFS) Remove(path string) error {
	_, err := exec.Command("podman", "exec", p.containerID, "rm", "-rf", path).Output()
	return err
}

func (p *podmanVFS) Rename(src, dst string) error {
	_, err := exec.Command("podman", "exec", p.containerID, "mv", src, dst).Output()
	return err
}

func (p *podmanVFS) MkdirAll(path string, perm fs.FileMode) error {
	_, err := exec.Command("podman", "exec", p.containerID, "mkdir", "-p", path).Output()
	return err
}

func (p *podmanVFS) Create(path string) (io.WriteCloser, error) {
	return &podmanWriter{containerID: p.containerID, path: path}, nil
}

type podmanWriter struct {
	containerID string
	path        string
	buf         []byte
}

func (pw *podmanWriter) Write(b []byte) (int, error) {
	pw.buf = append(pw.buf, b...)
	return len(b), nil
}

func (pw *podmanWriter) Close() error {
	cmd := exec.Command("podman", "exec", "-i", pw.containerID, "tee", pw.path)
	cmd.Stdin = strings.NewReader(string(pw.buf))
	_, err := cmd.Output()
	return err
}

func (p *podmanVFS) VFSName() string { return "podman://" + p.containerName }

// ListPodmanContainers returns a slice of container IDs + names for display
func ListPodmanContainers() ([]string, error) {
	out, err := exec.Command("podman", "ps", "--format", "{{.ID}} {{.Names}} {{.Status}}").Output()
	if err != nil {
		return nil, err
	}
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			result = append(result, line)
		}
	}
	return result, nil
}

// ─────────────────────────────────────────────
//  TAR VFS (read-only)
// ─────────────────────────────────────────────

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

func (t *tarVFS) Chdir(dir string) error              { return nil }
func (t *tarVFS) Getwd() (string, error)              { return t.filename, nil }
func (t *tarVFS) Remove(path string) error            { return fmt.Errorf("TAR is read-only") }
func (t *tarVFS) Rename(src, dst string) error        { return fmt.Errorf("TAR is read-only") }
func (t *tarVFS) MkdirAll(p string, _ fs.FileMode) error { return fmt.Errorf("TAR is read-only") }
func (t *tarVFS) Create(path string) (io.WriteCloser, error) {
	return nil, fmt.Errorf("TAR is read-only")
}
func (t *tarVFS) VFSName() string { return "tar://" + filepath.Base(t.filename) }

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

// ─────────────────────────────────────────────
//  ZIP VFS (read-only)
// ─────────────────────────────────────────────

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

func (z *zipVFS) Chdir(dir string) error              { return nil }
func (z *zipVFS) Getwd() (string, error)              { return z.filename, nil }
func (z *zipVFS) Remove(path string) error            { return fmt.Errorf("ZIP is read-only") }
func (z *zipVFS) Rename(src, dst string) error        { return fmt.Errorf("ZIP is read-only") }
func (z *zipVFS) MkdirAll(p string, _ fs.FileMode) error { return fmt.Errorf("ZIP is read-only") }
func (z *zipVFS) Create(path string) (io.WriteCloser, error) {
	return nil, fmt.Errorf("ZIP is read-only")
}
func (z *zipVFS) VFSName() string { return "zip://" + filepath.Base(z.filename) }

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
func (zf *zipFile) Stat() (fs.FileInfo, error) {
	return &zipFileInfo{name: zf.name, size: zf.size}, nil
}

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

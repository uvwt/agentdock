package artifactrelay

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type PreparedSource struct {
	Path        string
	Filename    string
	ContentType string
	Archive     bool
	Cleanup     func()
}

func PrepareSource(sourcePath, tempDir string) (PreparedSource, error) {
	absolute, err := filepath.Abs(sourcePath)
	if err != nil {
		return PreparedSource{}, err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return PreparedSource{}, fmt.Errorf("stat artifact source: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return PreparedSource{}, errors.New("artifact source cannot be a symbolic link")
	}
	if info.Mode().IsRegular() {
		return PreparedSource{Path: absolute, Filename: info.Name(), ContentType: "application/octet-stream", Cleanup: func() {}}, nil
	}
	if !info.IsDir() {
		return PreparedSource{}, errors.New("artifact source must be a regular file or directory")
	}
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		return PreparedSource{}, fmt.Errorf("create artifact temporary directory: %w", err)
	}
	temp, err := os.CreateTemp(tempDir, "artifact-directory-*.tar.gz")
	if err != nil {
		return PreparedSource{}, fmt.Errorf("create artifact archive: %w", err)
	}
	archivePath := temp.Name()
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		os.Remove(archivePath)
		return PreparedSource{}, err
	}
	if err := writeTarGzip(temp, absolute); err != nil {
		temp.Close()
		os.Remove(archivePath)
		return PreparedSource{}, err
	}
	if err := temp.Close(); err != nil {
		os.Remove(archivePath)
		return PreparedSource{}, err
	}
	return PreparedSource{
		Path: archivePath, Filename: filepath.Base(absolute) + ".tar.gz", ContentType: "application/gzip", Archive: true,
		Cleanup: func() { _ = os.Remove(archivePath) },
	}, nil
}

func writeTarGzip(output io.Writer, root string) error {
	gzipWriter, err := gzip.NewWriterLevel(output, gzip.BestSpeed)
	if err != nil {
		return err
	}
	gzipWriter.Header.ModTime = time.Unix(0, 0).UTC()
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	paths := []string{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("directory contains symbolic link %s", path)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("directory contains unsupported file type %s", path)
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		tarWriter.Close()
		gzipWriter.Close()
		return err
	}
	sort.Strings(paths)
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			tarWriter.Close()
			gzipWriter.Close()
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			tarWriter.Close()
			gzipWriter.Close()
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			tarWriter.Close()
			gzipWriter.Close()
			return err
		}
		header.Name = filepath.ToSlash(relative)
		header.ModTime = time.Unix(0, 0).UTC()
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}
		header.Uid, header.Gid = 0, 0
		header.Uname, header.Gname = "", ""
		if err := tarWriter.WriteHeader(header); err != nil {
			tarWriter.Close()
			gzipWriter.Close()
			return err
		}
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				tarWriter.Close()
				gzipWriter.Close()
				return err
			}
			_, copyErr := io.Copy(tarWriter, file)
			closeErr := file.Close()
			if copyErr != nil {
				tarWriter.Close()
				gzipWriter.Close()
				return copyErr
			}
			if closeErr != nil {
				tarWriter.Close()
				gzipWriter.Close()
				return closeErr
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		gzipWriter.Close()
		return err
	}
	return gzipWriter.Close()
}

func ExtractTarGzip(archivePath, destination string, maxFiles int, maxBytes int64) error {
	if maxFiles <= 0 {
		maxFiles = MaxExtractedFiles
	}
	if maxBytes <= 0 {
		maxBytes = MaxExtractedBytes
	}
	input, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer input.Close()
	gzipReader, err := gzip.NewReader(input)
	if err != nil {
		return errors.New("artifact archive is not a valid gzip stream")
	}
	defer gzipReader.Close()
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	root, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	reader := tar.NewReader(gzipReader)
	count := 0
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read artifact archive: %w", err)
		}
		count++
		if count > maxFiles {
			return errors.New("artifact archive contains too many entries")
		}
		name := filepath.Clean(filepath.FromSlash(header.Name))
		if name == "." || filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(os.PathSeparator)) {
			return errors.New("artifact archive contains an unsafe path")
		}
		target := filepath.Join(root, name)
		relative, err := filepath.Rel(root, target)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return errors.New("artifact archive path escapes destination")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || total+header.Size > maxBytes {
				return errors.New("artifact archive expands beyond the configured limit")
			}
			total += header.Size
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode) & 0o700
			if mode == 0 {
				mode = 0o600
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			written, copyErr := io.CopyN(file, reader, header.Size)
			closeErr := file.Close()
			if copyErr != nil || written != header.Size {
				return errors.New("artifact archive entry is truncated")
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return errors.New("artifact archive contains links or unsupported entries")
		}
	}
	return nil
}

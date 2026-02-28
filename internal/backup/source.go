package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/kolontsov/rbackup/internal/config"
)

// RunSource executes the source for a backup entry and streams the output to dst.
// Returns the number of bytes written to dst.
func RunSource(ctx context.Context, entry config.BackupEntry, settings config.Settings, dst io.Writer) (int64, error) {
	cw := &countWriter{w: dst}
	var err error
	switch {
	case entry.Cmd != "":
		err = runCmd(ctx, entry.Cmd, settings.CmdTimeoutD, cw)
	case entry.File != "":
		err = runFile(ctx, entry.File, cw)
	case entry.Dir != "":
		err = runDir(ctx, entry.Dir, cw)
	default:
		return 0, fmt.Errorf("no source configured")
	}
	return cw.n, err
}

func runCmd(ctx context.Context, cmdStr string, timeout time.Duration, w io.Writer) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", cmdStr)
	cmd.Stdout = w
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runFile(ctx context.Context, path string, dst io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	if _, err := copyContext(ctx, dst, f); err != nil {
		return fmt.Errorf("reading file: %w", err)
	}
	return nil
}

func runDir(ctx context.Context, dir string, dst io.Writer) error {
	gw := gzip.NewWriter(dst)
	tw := tar.NewWriter(gw)

	baseDir := filepath.Base(dir)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		linkTarget := ""
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err = os.Readlink(path)
			if err != nil {
				return fmt.Errorf("reading symlink target: %w", err)
			}
		}

		header, err := tar.FileInfoHeader(info, linkTarget)
		if err != nil {
			return fmt.Errorf("creating tar header: %w", err)
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		header.Name = filepath.Join(baseDir, rel)

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("writing tar header: %w", err)
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		if err := writeRegularFileToTar(ctx, tw, path); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("creating tar.gz: %w", err)
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("closing tar: %w", err)
	}
	if err := gw.Close(); err != nil {
		return fmt.Errorf("closing gzip: %w", err)
	}

	return nil
}

func writeRegularFileToTar(ctx context.Context, tw *tar.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}

	_, copyErr := copyContext(ctx, tw, f)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("writing file to tar: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing file: %w", closeErr)
	}
	return nil
}

func copyContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		if ctx.Err() != nil {
			return total, ctx.Err()
		}
		n, err := src.Read(buf)
		if n > 0 {
			nw, werr := dst.Write(buf[:n])
			total += int64(nw)
			if werr != nil {
				return total, werr
			}
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

type countWriter struct {
	w io.Writer
	n int64
}

func (cw *countWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.n += int64(n)
	return n, err
}

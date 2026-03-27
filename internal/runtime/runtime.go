package runtime

import (
	"archive/tar"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kushal/docksmith/internal/image"
)

type RunOptions struct {
	Name         string
	Tag          string
	Cmd          []string
	EnvOverrides []string
}

func Run(opts RunOptions) error {
	manifest, err := image.Load(opts.Name, opts.Tag)
	if err != nil {
		return err
	}

	command := manifest.Config.Cmd
	if len(opts.Cmd) > 0 {
		command = opts.Cmd
	}
	if len(command) == 0 {
		return fmt.Errorf("no CMD defined in image and no command provided")
	}

	rootDir, err := os.MkdirTemp("", "docksmith-root-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(rootDir)

	if err := ExtractLayers(manifest.Layers, rootDir); err != nil {
		return err
	}

	workdir := manifest.Config.WorkingDir
	if workdir == "" {
		workdir = "/"
	}
	os.MkdirAll(filepath.Join(rootDir, workdir), 0755)

	envMap := map[string]string{}
	for _, e := range manifest.Config.Env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	for _, e := range opts.EnvOverrides {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	var envSlice []string
	for k, v := range envMap {
		envSlice = append(envSlice, k+"="+v)
	}

	cmdStr := strings.Join(command, " ")
	return RunNamespaced(rootDir, workdir, envSlice, cmdStr)
}

func RunNamespaced(rootDir, workdir string, env []string, command string) error {
	args := []string{"__runtime__", rootDir, workdir, command}
	args = append(args, env...)

	cmd := exec.Command("/proc/self/exe", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:   syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS,
		Unshareflags: syscall.CLONE_NEWNS,
	}

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			fmt.Printf("Container exited with code %d\n", exitErr.ExitCode())
			return nil
		}
		return err
	}
	fmt.Println("Container exited with code 0")
	return nil
}

func ExecuteInNamespace(args []string) {
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "runtime: invalid args")
		os.Exit(1)
	}
	rootDir := args[0]
	workdir := args[1]
	command := args[2]
	env := args[3:]

	os.MkdirAll(filepath.Join(rootDir, "proc"), 0755)
	syscall.Mount("proc", filepath.Join(rootDir, "proc"), "proc", 0, "")

	if err := syscall.Chroot(rootDir); err != nil {
		fmt.Fprintf(os.Stderr, "chroot failed: %v\n", err)
		os.Exit(1)
	}
	if err := syscall.Chdir(workdir); err != nil {
		syscall.Chdir("/")
	}

	fullEnv := append(
		[]string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
		env...,
	)

	parts := strings.Fields(command)
	if len(parts) == 0 {
		fmt.Fprintln(os.Stderr, "runtime: empty command")
		os.Exit(1)
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = fullEnv

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "exec failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func ExtractLayers(layers []image.LayerMeta, destDir string) error {
	for _, l := range layers {
		if err := ExtractTar(image.LayerPath(l.Digest), destDir); err != nil {
			return fmt.Errorf("extracting layer %s: %w", l.Digest[:16], err)
		}
	}
	return nil
}

func ExtractTar(tarPath, destDir string) error {
	// Use system tar for better compatibility with Alpine's extended headers
	cmd := exec.Command("tar", "-xf", tarPath, "-C", destDir,
		"--no-same-owner",
		"--no-overwrite-dir",
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// fallback to pure Go tar
		return extractTarGo(tarPath, destDir)
	}
	return nil
}

func extractTarGo(tarPath, destDir string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			// skip unreadable headers (extended/vendor headers)
			continue
		}
		if hdr.Name == "" || hdr.Name == "./" || hdr.Name == "." {
			continue
		}
		target := filepath.Join(destDir, filepath.Clean("/"+hdr.Name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, fs.FileMode(hdr.Mode)|0755)
		case tar.TypeReg, tar.TypeRegA:
			os.MkdirAll(filepath.Dir(target), 0755)
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fs.FileMode(hdr.Mode)|0644)
			if err != nil {
				continue
			}
			io.Copy(out, tr)
			out.Close()
		case tar.TypeSymlink:
			os.Remove(target)
			os.Symlink(hdr.Linkname, target)
		case tar.TypeLink:
			linkTarget := filepath.Join(destDir, filepath.Clean("/"+hdr.Linkname))
			os.Remove(target)
			os.Link(linkTarget, target)
		}
	}
	return nil
}

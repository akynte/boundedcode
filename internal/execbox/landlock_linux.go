//go:build linux

package execbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// landlockBackend confines the filesystem with Landlock: the command is
// started by this program's helper, which restricts itself and then
// executes it. Reads are limited to the system, toolchains, caches and the
// project; writes to the project, /tmp, /dev and caches. The home
// directory, /run and other users' files cannot be opened.
type landlockBackend struct{}

func (landlockBackend) name() string { return "landlock" }

func landlockABI() int {
	v, _, e := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if e != 0 {
		return 0
	}
	return int(v)
}

func probeLandlock() string {
	if landlockABI() < 1 {
		return "Landlock is not enabled in this kernel"
	}
	if _, err := os.Executable(); err != nil {
		return "the helper cannot be located: " + err.Error()
	}
	return ""
}

func (landlockBackend) report(b *Box) Report {
	return Report{Backend: "landlock", Level: LevelPartial,
		Filesystem: fmt.Sprintf("Landlock ABI %d: the home directory, /run and other users' files cannot be opened, except toolchains "+
			"(read-only) and package caches; writes only to the project, /tmp and caches. Secret files inside the project and "+
			"git hooks are NOT protected (Landlock cannot hide a path beneath an allowed one)", landlockABI()),
		Network:   "not confined (Landlock filters TCP by port only, and not UDP)",
		Processes: "process group ended with the command; signals to processes outside it are blocked when the kernel supports scoping",
		Credentials: "secret-looking environment variables removed; credential files in the home directory cannot be opened; " +
			"Unix sockets (Docker, SSH agent) and other processes' /proc entries are NOT blocked"}
}

type landlockPolicy struct {
	Read  []string `json:"read"`
	Write []string `json:"write"`
}

const landlockEnv = "BOUNDEDCODE_LANDLOCK_POLICY"

var systemRead = []string{"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32", "/etc", "/opt", "/snap", "/nix", "/var/lib", "/var/cache",
	"/sys", "/proc", "/gnu"}

func (landlockBackend) wrap(b *Box, s Spec, _ *proxyHandle, tmp string) ([]string, []string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	hp := b.paths()
	pol := landlockPolicy{
		Read:  append(append(append([]string{}, systemRead...), hp.pathBinds...), hp.read...),
		Write: append(append([]string{b.root, tmp, "/tmp", "/dev"}, hp.write...), hp.gitRW...),
	}
	j, err := json.Marshal(pol)
	if err != nil {
		return nil, nil, err
	}
	return append([]string{exe, helperLandlock, "--"}, s.Argv...), []string{landlockEnv + "=" + string(j), "TMPDIR=" + tmp}, nil
}

// runLandlock is the helper: restrict this process, then become the
// command.
func runLandlock(argv []string) error {
	var pol landlockPolicy
	if err := json.Unmarshal([]byte(os.Getenv(landlockEnv)), &pol); err != nil {
		return fmt.Errorf("landlock policy: %w", err)
	}
	abi := landlockABI()
	if abi < 1 {
		return errors.New("landlock is not available")
	}
	fsAll := uint64(0x1fff) // ABI 1
	if abi >= 2 {
		fsAll |= unix.LANDLOCK_ACCESS_FS_REFER
	}
	if abi >= 3 {
		fsAll |= unix.LANDLOCK_ACCESS_FS_TRUNCATE
	}
	if abi >= 5 {
		fsAll |= unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	}
	attr := unix.LandlockRulesetAttr{Access_fs: fsAll}
	size := unsafe.Sizeof(attr)
	if abi >= 6 {
		attr.Scoped = unix.LANDLOCK_SCOPE_SIGNAL | unix.LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET
	} else {
		size = unsafe.Offsetof(attr.Scoped) // older kernels reject the larger struct
		if abi < 4 {
			size = unsafe.Offsetof(attr.Access_net)
		}
	}
	fd, _, e := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), size, 0)
	if e != 0 {
		return fmt.Errorf("landlock_create_ruleset: %w", e)
	}
	defer unix.Close(int(fd))
	fileRights := uint64(unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_READ_FILE)
	if abi >= 3 {
		fileRights |= unix.LANDLOCK_ACCESS_FS_TRUNCATE
	}
	if abi >= 5 {
		fileRights |= unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	}
	readRights := uint64(unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR)
	add := func(p string, rights uint64) error {
		pfd, err := unix.Open(p, unix.O_PATH|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil //nolint:nilerr // a path that does not exist is not granted
		}
		defer unix.Close(pfd)
		var st unix.Stat_t
		if unix.Fstat(pfd, &st) == nil && st.Mode&unix.S_IFMT != unix.S_IFDIR {
			rights &= fileRights
		}
		rule := unix.LandlockPathBeneathAttr{Allowed_access: rights, Parent_fd: int32(pfd)}
		if _, _, e := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, fd, unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&rule)), 0, 0, 0); e != 0 {
			return fmt.Errorf("landlock rule for %s: %w", p, e)
		}
		return nil
	}
	for _, p := range pol.Read {
		if err := add(p, readRights); err != nil {
			return err
		}
	}
	for _, p := range pol.Write {
		if err := add(p, fsAll); err != nil {
			return err
		}
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("no_new_privs: %w", err)
	}
	if _, _, e := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0); e != 0 {
		return fmt.Errorf("landlock_restrict_self: %w", e)
	}
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	if !filepath.IsAbs(bin) {
		if bin, err = filepath.Abs(bin); err != nil {
			return err
		}
	}
	var env []string
	for _, kv := range os.Environ() {
		if len(kv) < len(landlockEnv)+1 || kv[:len(landlockEnv)+1] != landlockEnv+"=" {
			env = append(env, kv)
		}
	}
	return syscall.Exec(bin, argv, env)
}

func platformBackends() []backend { return []backend{bwrapBackend{}, landlockBackend{}} }

func probeBackend(be backend) string {
	switch be.(type) {
	case bwrapBackend:
		return probeBwrap()
	case landlockBackend:
		return probeLandlock()
	}
	return "unknown backend"
}

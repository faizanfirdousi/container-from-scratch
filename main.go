package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// rootfsPath is the path to your extracted Linux root filesystem.
// Download one with:
//
//	docker export $(docker create alpine) -o alpine.tar
//	mkdir -p /home/faizan/alpine-rootfs && tar -xf alpine.tar -C /home/faizan/alpine-rootfs
const rootfsPath = "/home/faizan/alpine-rootfs"

// cgroupName is the name of the child cgroup we create for the container.
const cgroupName = "container-cfs"

// Usage: sudo go run main.go run <cmd> <args>
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s run <cmd> [args...]\n", os.Args[0])
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		run()
	case "child":
		child()
	default:
		panic("Unknown command. Usage: run <cmd> [args...]")
	}
}

func run() {
	fmt.Printf("Running %v \n", os.Args[2:])

	// Re-exec ourselves as "child" inside new namespaces.
	cmd := exec.Command("/proc/self/exe", append([]string{"child"}, os.Args[2:]...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.SysProcAttr = &syscall.SysProcAttr{
		// CLONE_NEWUTS  — isolate hostname
		// CLONE_NEWPID  — isolate PID numbering (PID 1 inside container)
		// CLONE_NEWNS   — isolate mount namespace so our mounts don't leak
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS,
		// Unshare the mount namespace so mount propagation doesn't escape.
		Unshareflags: syscall.CLONE_NEWNS,
	}

	must(cmd.Run())
}

func child() {
	fmt.Printf("Running %v \n", os.Args[2:])

	cmd := exec.Command(os.Args[2], os.Args[3:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Set essential environment variables so tools in the rootfs can be found.
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"TERM=xterm",
	}

	// Set hostname inside the UTS namespace
	must(syscall.Sethostname([]byte("container")))

	// Pivot into our rootfs — this gives us filesystem isolation
	must(syscall.Chroot(rootfsPath))
	must(os.Chdir("/"))

	// Mount /proc so tools like `ps` work inside the container.
	// This is safe because we're in our own mount + PID namespace.
	must(syscall.Mount("proc", "proc", "proc", 0, ""))

	// Mount a tmpfs at /tmp for scratch space
	must(os.MkdirAll("tmp", 0755))
	must(syscall.Mount("tmpfs", "tmp", "tmpfs", 0, ""))

	// Set up cgroup v2 resource limits
	cg()

	// Run the command. We don't use must() here because the user might
	// exit the shell with Ctrl+C or a non-zero exit code, and that's fine.
	if err := cmd.Run(); err != nil {
		fmt.Println("Process exited:", err)
	}

	// Clean up mounts on exit (ignore errors — they may already be unmounted)
	syscall.Unmount("proc", 0)
	syscall.Unmount("tmp", 0)
}

// cg sets up cgroup v2 resource limits for the container process.
//
// Cgroup v2 uses a UNIFIED hierarchy — all controllers live under a single
// tree at /sys/fs/cgroup/. This is different from v1 which had separate
// trees per controller (e.g. /sys/fs/cgroup/pids/, /sys/fs/cgroup/memory/).
//
// Steps:
//  1. Enable controllers on the root cgroup (subtree_control)
//  2. Create a child cgroup directory for our container
//  3. Write resource limits to the child cgroup
//  4. Move our process into the child cgroup
func cg() {
	cgroupRoot := "/sys/fs/cgroup/"
	containerCgroup := filepath.Join(cgroupRoot, cgroupName)

	// Step 1: Enable pids, memory, and cpu controllers on the root cgroup.
	// Writing "+controller" to subtree_control makes that controller available
	// to child cgroups.
	// Note: This may fail if controllers are already enabled or if the system
	// doesn't support all controllers. We ignore errors here for flexibility.
	os.WriteFile(
		filepath.Join(cgroupRoot, "cgroup.subtree_control"),
		[]byte("+pids +memory +cpu"),
		0644,
	)

	// Step 2: Create a child cgroup directory for our container.
	// Each directory under /sys/fs/cgroup/ is a cgroup. The kernel
	// automatically populates it with control files.
	must(os.MkdirAll(containerCgroup, 0755))

	// Step 3: Set resource limits by writing to the control files.

	// pids.max — Limit the number of processes to prevent fork bombs.
	// "20" means at most 20 processes can exist in this cgroup.
	must(os.WriteFile(filepath.Join(containerCgroup, "pids.max"), []byte("20"), 0700))

	// memory.max — Limit memory usage (in bytes).
	// "52428800" = 50 MiB. The OOM killer activates if exceeded.
	must(os.WriteFile(filepath.Join(containerCgroup, "memory.max"), []byte("52428800"), 0700))

	// cpu.max — Limit CPU time.
	// Format: "$MAX $PERIOD" (microseconds). "50000 100000" means the cgroup
	// can use 50ms out of every 100ms period = 50% of one CPU core.
	must(os.WriteFile(filepath.Join(containerCgroup, "cpu.max"), []byte("50000 100000"), 0700))

	// Step 4: Move the current process into the child cgroup.
	// Writing our PID to cgroup.procs makes this process (and all its
	// future children) subject to the limits above.
	must(os.WriteFile(
		filepath.Join(containerCgroup, "cgroup.procs"),
		[]byte(strconv.Itoa(os.Getpid())),
		0700,
	))

	// Note: In cgroup v2, there is no "notify_on_release". Cleanup of the
	// cgroup directory must be done manually (e.g. rmdir the directory after
	// the container process exits). For this learning demo, we leave cleanup
	// to the user: sudo rmdir /sys/fs/cgroup/container-cfs
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

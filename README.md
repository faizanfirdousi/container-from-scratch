Here is a shorter version that keeps the essentials while pointing readers to your blog for deeper explanation:

---

# Container From Scratch (Go)

A minimal container runtime built in Go using core Linux primitives — without Docker.

This project demonstrates how containers work internally using:

* Linux namespaces (PID, UTS, Mount)
* `chroot` for filesystem isolation
* `/proc` and `tmpfs` mounting
* cgroup v2 for CPU, memory, and process limits

It re-executes itself inside new namespaces, pivots into a minimal root filesystem, applies resource constraints, and runs a command as PID 1 inside the container.

For a detailed explanation of how each part works, read the accompanying blog post.

---

## Requirements

* Linux system
* Go installed
* cgroup v2 enabled
* Root privileges (`sudo`)

---

## Setup Root Filesystem

```bash
docker export $(docker create alpine) -o alpine.tar
mkdir -p ~/alpine-rootfs
tar -xf alpine.tar -C ~/alpine-rootfs
```

Update the `rootfsPath` in the code accordingly.

---

## Usage

```bash
sudo go run main.go run /bin/sh
```

Example:

```bash
sudo go run main.go run /bin/echo hello
```

---

## Note

This is a learning project to understand container internals. It is not production-ready.

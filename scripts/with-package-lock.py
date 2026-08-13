#!/usr/bin/env python3
import fcntl
import os
from pathlib import Path
import stat
import sys


def resolve_package_root():
    override = os.environ.get("BATUTA_PACKAGE_ROOT")
    if override:
        root = Path(override)
    else:
        data_home = os.environ.get("XDG_DATA_HOME")
        root = (
            Path(data_home) / "batuta-compozy" / "packages"
            if data_home
            else Path.home() / ".local" / "share" / "batuta-compozy" / "packages"
        )
    if not root.is_absolute():
        raise SystemExit(f"package root must be absolute: {root}")
    root.mkdir(parents=True, exist_ok=True)
    if root.is_symlink() or not root.is_dir():
        raise SystemExit(f"package root must be a directory, not a symlink: {root}")
    return root.resolve(strict=True)


def inherited_lock_matches(fd_text, path_text):
    try:
        descriptor = os.fstat(int(fd_text))
        lock_file = os.stat(path_text, follow_symlinks=False)
    except (OSError, TypeError, ValueError):
        return False
    return (
        stat.S_ISREG(descriptor.st_mode)
        and stat.S_ISREG(lock_file.st_mode)
        and (descriptor.st_dev, descriptor.st_ino) == (lock_file.st_dev, lock_file.st_ino)
    )


if len(sys.argv) == 4 and sys.argv[1] == "--check":
    raise SystemExit(0 if inherited_lock_matches(sys.argv[2], sys.argv[3]) else 1)


if len(sys.argv) < 2:
    raise SystemExit(f"usage: {sys.argv[0]} COMMAND [ARG ...]")

package_root = resolve_package_root()
lock_path = package_root / ".publication.lock"
flags = os.O_CREAT | os.O_RDWR
if hasattr(os, "O_NOFOLLOW"):
    flags |= os.O_NOFOLLOW
lock_fd = os.open(lock_path, flags, 0o600)
if not stat.S_ISREG(os.fstat(lock_fd).st_mode):
    raise SystemExit(f"package lock is not a regular file: {lock_path}")
fcntl.flock(lock_fd, fcntl.LOCK_EX)
os.set_inheritable(lock_fd, True)

environment = os.environ.copy()
environment["BATUTA_PACKAGE_ROOT"] = str(package_root)
environment["BATUTA_PACKAGE_LOCK_FD"] = str(lock_fd)
environment["BATUTA_PACKAGE_LOCK_PATH"] = str(lock_path)
os.execvpe(sys.argv[1], sys.argv[1:], environment)

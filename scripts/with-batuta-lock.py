#!/usr/bin/env python3
import fcntl
import os
from pathlib import Path
import pwd
import stat
import sys


def require_absolute_directory(path, *, private):
    if not path.is_absolute():
        raise SystemExit(f"lock root must be absolute: {path}")
    path.mkdir(parents=True, exist_ok=True, mode=0o700 if private else 0o755)
    if path.is_symlink() or not path.is_dir():
        raise SystemExit(f"lock root must be a directory, not a symlink: {path}")
    resolved = path.resolve(strict=True)
    metadata = resolved.stat()
    if metadata.st_uid != os.getuid():
        raise SystemExit(f"lock root must be owned by the current user: {resolved}")
    if private:
        os.chmod(resolved, 0o700)
    return resolved


def package_lock():
    override = os.environ.get("BATUTA_PACKAGE_ROOT")
    if override:
        package_root = Path(override)
    else:
        data_home = os.environ.get("XDG_DATA_HOME")
        package_root = (
            Path(data_home) / "batuta-compozy" / "packages"
            if data_home
            else Path.home() / ".local" / "share" / "batuta-compozy" / "packages"
        )
    package_root = require_absolute_directory(package_root, private=False)
    return (
        package_root / ".publication.lock",
        {
            "BATUTA_PACKAGE_ROOT": str(package_root),
            "BATUTA_PACKAGE_LOCK_PATH": str(package_root / ".publication.lock"),
        },
    )


def republish_lock():
    override = os.environ.get("BATUTA_REPUBLISH_LOCK_ROOT")
    lock_root = (
        Path(override)
        if override
        else Path(pwd.getpwuid(os.getuid()).pw_dir) / ".compozy" / "locks"
    )
    lock_root = require_absolute_directory(lock_root, private=True)
    return (
        lock_root / "batuta-republish.lock",
        {
            "BATUTA_REPUBLISH_LOCK_PATH": str(lock_root / "batuta-republish.lock"),
        },
    )


def inherited_lock_matches(fd_text, path_text):
    try:
        descriptor = os.fstat(int(fd_text))
        lock_file = os.stat(path_text, follow_symlinks=False)
    except (OSError, TypeError, ValueError):
        return False
    return (
        stat.S_ISREG(descriptor.st_mode)
        and stat.S_ISREG(lock_file.st_mode)
        and (
            (descriptor.st_dev, descriptor.st_ino)
            == (lock_file.st_dev, lock_file.st_ino)
        )
    )


if len(sys.argv) == 4 and sys.argv[1] == "--check":
    raise SystemExit(0 if inherited_lock_matches(sys.argv[2], sys.argv[3]) else 1)
if len(sys.argv) < 3 or sys.argv[1] not in {"package", "republish"}:
    raise SystemExit(f"usage: {sys.argv[0]} package|republish COMMAND [ARG ...]")

kind = sys.argv[1]
lock_path, environment_updates = (
    package_lock() if kind == "package" else republish_lock()
)
flags = os.O_CREAT | os.O_RDWR
if hasattr(os, "O_NOFOLLOW"):
    flags |= os.O_NOFOLLOW
lock_fd = os.open(lock_path, flags, 0o600)
lock_metadata = os.fstat(lock_fd)
if not stat.S_ISREG(lock_metadata.st_mode) or lock_metadata.st_uid != os.getuid():
    raise SystemExit(f"lock must be a regular file owned by the current user: {lock_path}")
os.fchmod(lock_fd, 0o600)

attempt_fd = (
    os.environ.get("BATUTA_REPUBLISH_LOCK_ATTEMPT_FD")
    if kind == "republish"
    else None
)
if attempt_fd is not None:
    os.write(int(attempt_fd), b"attempt\n")
fcntl.flock(lock_fd, fcntl.LOCK_EX)
os.set_inheritable(lock_fd, True)

environment = os.environ.copy()
environment.pop("BATUTA_REPUBLISH_LOCK_ATTEMPT_FD", None)
environment.update(environment_updates)
environment[f"BATUTA_{kind.upper()}_LOCK_FD"] = str(lock_fd)
os.execvpe(sys.argv[2], sys.argv[2:], environment)

"""Own a loopback PostgreSQL cluster; never accept a deployment database URI."""

import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import tempfile
import urllib.parse


def private_json(path, value):
    # Never expose credentials through the process umask, even briefly. A failed
    # write must also leave the previous baseline/ownership record intact.
    descriptor, temporary = tempfile.mkstemp(prefix=".private-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w") as output:
            output.write(json.dumps(value, indent=2) + "\n")
        os.replace(temporary, path)
    finally:
        Path(temporary).unlink(missing_ok=True)


def unused_port():
    with socket.socket() as connection:
        connection.bind(("127.0.0.1", 0))
        return connection.getsockname()[1]


def digest_file(path):
    digest = hashlib.sha256()
    with path.open("rb") as source:
        while block := source.read(8 * 1024 * 1024):
            digest.update(block)
    return digest.hexdigest()


class Database:
    def __init__(self, root, pg_bin=None):
        self.root = root.resolve()
        if root.is_symlink() or any(p.is_symlink() for p in root.parents if p.exists()):
            raise ValueError("Performance workspace must not be a symbolic link")
        self.root.mkdir(mode=0o700, parents=True, exist_ok=True)
        self.state_path = self.root / "owner.json"
        if self.state_path.exists():
            self.state = json.loads(self.state_path.read_text())
            if self.state["root"] != str(self.root) or self.state["uid"] != os.getuid():
                raise ValueError("Performance workspace owner mismatch")
        else:
            if any(self.root.iterdir()):
                raise ValueError("Refusing an unowned performance workspace")
            binary = pg_bin or subprocess.check_output(["pg_config", "--bindir"], text=True).strip()
            self.state = {"root": str(self.root), "uid": os.getuid(), "token": secrets.token_hex(24), "port": unused_port(), "password": secrets.token_urlsafe(32), "pg_bin": str(Path(binary).resolve())}
            area = Path(tempfile.mkdtemp(prefix="aiosources-performance-pg-"))
            self.state["database_area"] = str(area.resolve())
            private_json(area / "owner.json", {"token": self.state["token"], "uid": os.getuid()})
            private_json(self.state_path, self.state)
        self.root.chmod(0o700)
        self.pg = Path(self.state["pg_bin"])
        version = subprocess.check_output([str(self.pg / "pg_config"), "--version"], text=True)
        if not version.startswith("PostgreSQL 18."):
            raise ValueError("PostgreSQL 18 is required for reproducible performance checks")
        if not self.state.get("database_area"):
            raise ValueError("Prepare a fresh performance workspace with the current runner")
        area = Path(self.state["database_area"])
        expected_parent = Path(tempfile.gettempdir()).resolve()
        if area.is_symlink() or area.parent != expected_parent or not area.name.startswith("aiosources-performance-pg-") or not area.is_dir() or area.stat().st_uid != os.getuid():
            raise ValueError("Owned temporary database area is missing or invalid; prepare a new snapshot")
        marker = json.loads((area / "owner.json").read_text())
        if marker != {"token": self.state["token"], "uid": os.getuid()}:
            raise ValueError("Temporary database owner mismatch")
        self.log = area / "database.log"
        self.data = area / "postgres"
        self.running = False

    def environment(self, name="postgres"):
        return {**{key: value for key, value in os.environ.items() if not key.startswith("PG")}, "PGHOST": "127.0.0.1", "PGPORT": str(self.state["port"]), "PGUSER": "performance_owner", "PGPASSWORD": self.state["password"], "PGDATABASE": name, "PGCONNECT_TIMEOUT": "10", "PGOPTIONS": ""}

    def uri(self, name="postgres"):
        return "postgresql://performance_owner:" + urllib.parse.quote(self.state["password"], safe="") + "@127.0.0.1:" + str(self.state["port"]) + "/" + name

    def command(self, args, *, name="postgres", input=None, timeout=1800):
        with self.log.open("ab") as output:
            result = subprocess.run([str(self.pg / args[0]), *map(str, args[1:])], input=input, stdout=output, stderr=output, env=self.environment(name), timeout=timeout)
        self.log.chmod(0o600)
        if result.returncode:
            raise RuntimeError("Private test database operation failed; diagnostic stays in the local workspace")

    def start(self):
        if not self.data.exists():
            if (self.root / "prepared.json").exists():
                raise ValueError("The retained temporary database is missing; prepare a new snapshot")
            password_file = self.root / "init-password"
            password_file.write_text(self.state["password"])
            password_file.chmod(0o600)
            try:
                self.command(["initdb", "-D", self.data, "-U", "performance_owner", "--pwfile", password_file, "--auth=scram-sha-256", "--encoding=UTF8", "--no-locale"])
            finally:
                password_file.unlink(missing_ok=True)
            with (self.data / "postgresql.conf").open("a") as settings:
                settings.write("\nlisten_addresses='127.0.0.1'\nport=" + str(self.state["port"]) + "\nunix_socket_directories=''\nio_method='sync'\nshared_buffers='256MB'\nwork_mem='4MB'\nmaintenance_work_mem='256MB'\nmax_wal_size='1GB'\nwal_compression=on\nmax_connections=80\n")
        if (self.data / "postmaster.pid").exists():
            raise RuntimeError("Performance PostgreSQL is already running; refusing to share a measurement session")
        self.command(["pg_ctl", "-D", self.data, "-l", self.data.parent / "postgres.log", "start", "-w", "-t", "30"], timeout=45)
        self.running = True

    def stop(self):
        if self.running:
            self.command(["pg_ctl", "-D", self.data, "stop", "-m", "fast", "-w", "-t", "30"], timeout=40)
            self.running = False

    def prepare(self, dump):
        if (self.root / "dataset.json").exists():
            raise ValueError("This workspace already holds a dataset; use a separate workspace for a new dump")
        digest = digest_file(dump)
        # Do not attempt a large restore on an almost-full development disk.
        if shutil.disk_usage(self.data.parent).free < max(8 * 1024**3, dump.stat().st_size * 8):
            raise ValueError("Insufficient space for an isolated dump restore; retain headroom for indexes and WAL")
        self.command(["createdb", "performance_snapshot"])
        self.command(["pg_restore", "--no-owner", "--no-privileges", "--exit-on-error", "--jobs=2", "--dbname=performance_snapshot", dump], name="performance_snapshot", timeout=3600)
        private_json(self.root / "dataset.json", {"sha256": digest, "archive_bytes": dump.stat().st_size})
        self.command(["psql", "-X", "-v", "ON_ERROR_STOP=1", "-c", "ANALYZE"], name="performance_snapshot")


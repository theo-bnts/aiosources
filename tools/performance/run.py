#!/usr/bin/env python3
"""Opt-in, local-only regression gate over a disposable private snapshot."""

import argparse
from collections import defaultdict
import json
import os
from pathlib import Path
import platform
import re
import secrets
import signal
import statistics
import subprocess
import sys
import threading
import time
import traceback
import urllib.request

from database import Database, digest_file, private_json, unused_port
from compatibility import default_workspace
from report import SCHEMA, evaluate, summarize

REPOSITORY = Path(__file__).resolve().parents[2]
TOOLS = Path(__file__).resolve().parent
REQUIRED_BENCHMARKS = {
    "synthetic.books.BenchmarkBookRelease",
    "synthetic.connectors.BenchmarkJSONCollectionPage", "synthetic.connectors.BenchmarkHTMLCollectionPage", "synthetic.connectors.BenchmarkTorznabCollectionPage",
    "synthetic.jobs.BenchmarkSourceImport/full", "synthetic.jobs.BenchmarkSourceImport/incremental",
    "synthetic.store.BenchmarkCatalogReadPaths/title-search", "synthetic.store.BenchmarkAdministrationPaths/catalog-export",
    "synthetic.aiosource.BenchmarkMatchingDenseCatalog", "synthetic.aiosource.BenchmarkMatchingVariedCatalog",
    "synthetic.aiosource.BenchmarkMetadataRetrieval", "synthetic.aiosource.BenchmarkMetadataInventoryReplay",
    "synthetic.aiosource.BenchmarkGameInformation", "synthetic.aiosource.BenchmarkGameMatching",
    "synthetic.aiosource.BenchmarkBookImport", "synthetic.aiosource.BenchmarkBookChanges",
    "synthetic.aiosource.BenchmarkInformationReset/books", "synthetic.aiosource.BenchmarkInformationReset/videos",
    "synthetic.httpapi.BenchmarkAddonReadFlows/private-stremio", "synthetic.httpapi.BenchmarkAddonReadFlows/public-stremio",
    "synthetic.httpapi.BenchmarkAddonReadFlows/private-torznab", "synthetic.httpapi.BenchmarkAddonReadFlows/public-torznab",
    "synthetic.httpapi.BenchmarkAddonReadFlows/private-sharing", "synthetic.httpapi.BenchmarkAddonReadFlows/public-sharing",
    "synthetic.httpapi.BenchmarkAddonReadFlows/public-download",
}
REQUIRED_BENCHMARKS.update(
    "synthetic.store.BenchmarkStorageChoices/" + name
    for name in ("required-only", "additional-field", "all-fields", "successful-records", "record-field-copies", "published-history")
)
REQUIRED_BENCHMARKS.update(
    "synthetic.httpapi.BenchmarkAddonReadFlows/" + name
    for name in ("private-direct-search", "public-direct-search")
)
REQUIRED_BENCHMARKS.update(
    "synthetic.aiosource.BenchmarkProviderAccountValidation/" + service
    for service in ("alldebrid", "debridlink", "deepbrid", "premiumize", "realdebrid", "torbox")
)
REQUIRED_BENCHMARKS.update(
    "synthetic.httpapi.BenchmarkAddonReadFlows/" + name
    for name in ("private-search", "public-search", "private-releases", "public-releases", "private-manifest", "public-manifest", "identification", "authentication")
)


def child_environment(database, **extra):
    # In particular, do not inherit a deployment's DATABASE_URL or ADMIN_PASSWORD.
    env = {key: value for key, value in os.environ.items() if not key.startswith("PG") and key not in ("DATABASE_URL", "ADMIN_PASSWORD", "DATA_DIR", "PROVIDERS_DIR", "BIND", "PUBLIC_URL", "TEST_DATABASE_URL")}
    env.update(PERFORMANCE_WORKSPACE=str(database.root), PERFORMANCE_DATABASE_URL=database.uri("performance_snapshot"))
    env.update(extra)
    return env


def invoke(database, command, name, env, *, timeout=900, check=True):
    log = database.root / (name + ".log")
    with log.open("wb") as output:
        child = subprocess.Popen(command, env=env, cwd=REPOSITORY, stdout=output, stderr=output, start_new_session=True)
        try:
            code = child.wait(timeout=timeout)
        finally:
            # A timed-out browser/Go invocation can leave descendants alive.
            # This is the new process group owned by this invocation only.
            try:
                os.killpg(child.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                child.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGKILL)
                child.wait(timeout=5)
    log.chmod(0o600)
    if check and code:
        raise RuntimeError("Local " + name + " failed; diagnostic stays in the private workspace")
    return code


def parse_go_benchmarks(output):
    groups = defaultdict(list)
    allocations = defaultdict(list)
    package, name = "", None
    for line in output.splitlines():
        if line.startswith("pkg: "):
            package = line.rsplit("/", 1)[-1]
            name = None
        header = re.match(r"(Benchmark\S+?)(?:-\d+)?\s", line)
        if header:
            name = "synthetic." + package + "." + header[1]
        # Go prints the name before executing a benchmark. Background log
        # messages can intervene before its numerical result on the next line.
        match = re.search(r"\s\d+\s+([0-9.]+)\s+ns/op\b(.*)", line)
        if match and name:
            groups[name].append(float(match[1]) / 1_000_000)
            tail = match[2]
            memory = re.search(r"([0-9.]+) B/op\s+([0-9.]+) allocs/op", tail)
            if memory:
                allocations[name].append((float(memory[1]), float(memory[2])))
            name = None
    return groups, allocations


def go_benchmarks(database, samples):
    env = child_environment(database, TEST_DATABASE_URL=database.uri(), GOMAXPROCS="2", GOMEMLIMIT="512MiB")
    common = [os.environ.get("GO", "go"), "test", "-run=^$", "-bench=.", "-benchmem", "-count=" + str(samples), "-timeout=10m"]
    # One parse is too small to amortize regexp/sync.Pool initialization after
    # Go's benchmark GC. Keep expensive complete operations at one iteration.
    micro = invoke(database, [*common, "-benchtime=100x", "./internal/books"], "synthetic-books", env, timeout=120, check=False)
    accounts = invoke(database, [*common, "-bench=^BenchmarkProviderAccountValidation$", "-benchtime=100x", "./internal/aiosource"], "synthetic-accounts", env, timeout=120, check=False)
    code = invoke(database, [*common, "-benchtime=1x", "./internal/connectors", "./internal/jobs", "./internal/store", "./internal/aiosource", "./internal/httpapi"], "synthetic", env, timeout=900, check=False)
    if code or micro or accounts:
        raise RuntimeError("Synthetic operation checks failed; private logs retained")
    groups, allocations = parse_go_benchmarks((database.root / "synthetic.log").read_text())
    # The complete-operation log also includes cold account checks. Use the
    # independent warm profile for these stateless requests, whose HTTP buffer
    # pools would otherwise dominate a single call's allocation count.
    profiles = {
        "synthetic-books": {"synthetic.books.BenchmarkBookRelease"},
        "synthetic-accounts": {name for name in REQUIRED_BENCHMARKS if ".BenchmarkProviderAccountValidation/" in name},
    }
    for name, required in profiles.items():
        measured, memory = parse_go_benchmarks((database.root / (name + ".log")).read_text())
        if not required.issubset(measured):
            raise RuntimeError("Warm benchmark profile is incomplete")
        groups.update(measured)
        allocations.update(memory)
    if not REQUIRED_BENCHMARKS.issubset(groups) or any(len(values) != samples or len(allocations[name]) != samples for name, values in groups.items()):
        raise RuntimeError("Synthetic benchmark coverage or repetition count is incomplete")
    result = []
    for name, values in groups.items():
        # Batch imports/matching legitimately do more work than a page request.
        budget = 15_000 if any(value in name for value in ("Matching", "MetadataRetrieval", "BookImport", "Reset", "CatalogRoundTrip")) else 5000
        row = {"id": name, "budget_ms": budget, "samples_ms": values, "noise_ms": 20, "contract": "asserted"}
        # Full one-iteration flows can acquire a pooled 16 KiB HTTP/PG buffer
        # after benchmark GC. Warm micro profiles retain the tighter allowance.
        row["noise_bytes"] = 4096 if name.startswith("synthetic.books.") or ".BenchmarkProviderAccountValidation/" in name else 32768
        if allocations[name]:
            row.update(bytes_per_op=statistics.median(value[0] for value in allocations[name]), allocs_per_op=statistics.median(value[1] for value in allocations[name]))
        result.append(row)
    return result


def process_seconds(value):
    days, _, tail = value.rpartition("-")
    parts = [float(part) for part in tail.split(":")]
    result = 0
    for part in parts:
        result = result * 60 + part
    return result + (int(days) * 86400 if days else 0)


class Resources:
    """Sample only the owned app and PostgreSQL descendants, not the whole host."""
    def __init__(self, app_pid, postgres_pid):
        self.roots = {app_pid, postgres_pid}
        self.app_pid = app_pid
        self.stop_event = threading.Event()
        self.peak = 0
        self.cpu = {}
        self.initial = {}
        self.thread = threading.Thread(target=self.sample, daemon=True)
        self.thread.start()

    def sample(self):
        while not self.stop_event.is_set():
            try:
                lines = subprocess.check_output(["ps", "-axo", "pid=,ppid=,rss=,time="], text=True).splitlines()
                rows = [(*map(int, line.split()[:3]), process_seconds(line.split()[3])) for line in lines if len(line.split()) == 4]
                selected = set(self.roots)
                changed = True
                while changed:
                    before = len(selected)
                    selected.update(pid for pid, parent, _, _ in rows if parent in selected)
                    changed = before != len(selected)
                first = not self.cpu
                for pid, _, _, cpu in rows:
                    if pid in selected:
                        if first and pid != self.app_pid:
                            self.initial[pid] = cpu
                        self.cpu[pid] = max(self.cpu.get(pid, 0), cpu)
                self.peak = max(self.peak, sum(rss for pid, _, rss, _ in rows if pid in selected) * 1024)
            except (OSError, ValueError, subprocess.SubprocessError):
                pass
            self.stop_event.wait(.5)

    def close(self):
        self.stop_event.set()
        self.thread.join(3)
        if self.peak <= 0:
            raise RuntimeError("Owned process memory could not be measured")
        return {"peak_rss_mib": round(self.peak / 1024**2, 2), "sampled_cpu_seconds": round(sum(max(0, cpu - self.initial.get(pid, 0)) for pid, cpu in self.cpu.items()), 3)}


def measure_application(database, binary, samples):
    port = unused_port()
    origin = "http://127.0.0.1:" + str(port)
    password = secrets.token_urlsafe(32)
    state = database.root / "state"
    env = {key: os.environ[key] for key in ("PATH", "SYSTEMROOT") if key in os.environ}
    env.update(HOME=str(state), TMPDIR=str(database.root), LANG="C.UTF-8", TZ="UTC", GOMAXPROCS="2", GOMEMLIMIT="512MiB", DATABASE_URL=database.uri("performance_snapshot"), ADMIN_PASSWORD=password)
    log = database.root / "application.log"
    started = time.monotonic()
    child = None
    resources = None
    resource_values = {}
    result = []
    try:
        with log.open("wb") as output:
            child = subprocess.Popen([str(binary), "serve", "--bind", "127.0.0.1:" + str(port), "--public-url", origin, "--data-dir", str(state), "--providers", str(database.root / "providers"), "--workers", "1"], env=env, stdout=output, stderr=output)
        log.chmod(0o600)
        postgres_pid = int((database.data / "postmaster.pid").read_text().splitlines()[0])
        resources = Resources(child.pid, postgres_pid)
        ready = False
        while time.monotonic() - started < 45:
            if child.poll() is not None:
                raise RuntimeError("Owned application exited before readiness; private startup diagnostic retained")
            # Do not connect to any port until this owned process confirms its bind.
            if ("AIOSources ready at " + origin + " (version ") in log.read_text(errors="replace"):
                with urllib.request.urlopen(origin + "/healthz", timeout=5) as response:
                    ready = response.status == 200
                if ready:
                    break
            time.sleep(.05)
        if not ready:
            raise RuntimeError("Owned application exceeded its startup deadline")
        result.append({"id": "application.startup", "budget_ms": 30_000, "samples_ms": [(time.monotonic() - started) * 1000], "noise_ms": 1000, "contract": "ready"})
        profile = child_environment(database, PERFORMANCE_ORIGIN=origin, PERFORMANCE_ADMIN_PASSWORD=password, PERFORMANCE_SAMPLES=str(samples))
        (database.root / "measurements-private.json").unlink(missing_ok=True)
        invoke(database, ["node", str(TOOLS / "measure.mjs")], "measure", profile, timeout=1200, check=False)
        result.extend(json.loads((database.root / "measurements-private.json").read_text()))
    finally:
        if child and child.poll() is None:
            child.send_signal(signal.SIGTERM)
            try:
                child.wait(timeout=35)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=5)
        if resources:
            resource_values = resources.close()
    return result, resource_values


def environment_identity(samples):
    # No hostname, paths, user name, connection URI or credential enters a report.
    return {"os": platform.system(), "architecture": platform.machine(), "cpu_count": os.cpu_count(), "app_threads": 2, "go_memory_mib": 512, "pg_shared_buffers_mib": 256, "pg_work_mem_mib": 4, "pg_io_method": "sync", "pg_wal_mib": 1024, "book_iterations": 100, "account_iterations": 100, "operation_iterations": 1, "operation_allocation_noise_bytes": 32768, "relative_tail_samples": 2, "browser_context": "per-document-navigation", "browser_navigation_noise_ms": 350, "browser_control_noise_ms": 150, "samples": samples}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("prepare", "run", "check", "if-configured", "configured"))
    parser.add_argument("--workspace", type=Path, default=default_workspace(REPOSITORY))
    parser.add_argument("--pg-bin", type=Path)
    parser.add_argument("--dump", type=Path)
    parser.add_argument("--binary", type=Path, default=REPOSITORY / "bin/aiosources")
    parser.add_argument("--label", default="current")
    parser.add_argument("--baseline", type=Path)
    parser.add_argument("--save-baseline", action="store_true")
    parser.add_argument("--samples", type=int, default=5)
    args = parser.parse_args()
    workspace = args.workspace.absolute()
    if args.command == "configured":
        return 0 if not os.environ.get("CI") and (workspace / "baseline.json").is_file() else 1
    if args.command == "if-configured":
        if os.environ.get("CI") or not (workspace / "baseline.json").exists():
            print("Private performance gate is local and has not been configured here.")
            return 0
        args.command = "check"
    if os.environ.get("CI"):
        raise ValueError("Private snapshot performance checks must never run in CI")
    if not re.fullmatch(r"[a-zA-Z0-9_-]{1,80}", args.label) or not 3 <= args.samples <= 20:
        raise ValueError("Use a simple report label and 3–20 measured repetitions")
    if args.command == "prepare" and (not args.dump or not args.dump.is_file()):
        raise ValueError("A readable private PostgreSQL dump is required")
    if args.command != "prepare" and not (workspace / "prepared.json").is_file():
        raise ValueError("Prepare a disposable private snapshot first")
    if args.command != "prepare" and not args.binary.resolve().is_file():
        raise ValueError("Build an embedded production binary before measuring")
    database = Database(workspace, args.pg_bin)
    try:
        database.start()
        if args.command == "prepare":
            database.prepare(args.dump.resolve())
            invoke(database, ["node", str(TOOLS / "bootstrap.mjs")], "isolation", child_environment(database))
            print("Private snapshot prepared, automatic jobs disabled and stored secrets replaced with synthetic values.")
            return 0
        report_path = workspace / "reports" / (args.label + ".json")
        report_path.parent.mkdir(mode=0o700, exist_ok=True)
        baseline_path = args.baseline or (workspace / "baseline.json" if args.command == "check" else None)
        if baseline_path and baseline_path.resolve() == report_path.resolve():
            raise ValueError("Candidate output must not overwrite its baseline")
        baseline = json.loads(baseline_path.read_text()) if baseline_path else None
        cases = go_benchmarks(database, args.samples)
        application, resources = measure_application(database, args.binary.resolve(), args.samples)
        cases.extend(application)
        revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=REPOSITORY, text=True).strip()
        binary_hash = digest_file(args.binary)
        report = {"schema": SCHEMA, "suite": 1, "dataset": json.loads((workspace / "dataset.json").read_text())["sha256"], "environment": environment_identity(args.samples), "revision": revision,
                  "binary_sha256": binary_hash, "resources": resources, "cases": [summarize(case) for case in cases]}
        failures = evaluate(report, baseline)
        report["failures"] = failures
        private_json(report_path, report)
        for failure in failures:
            print(json.dumps(failure))
        print(json.dumps({"cases": len(cases), "failed": len(failures), "compared": baseline is not None}))
        if failures:
            return 1
        if args.save_baseline:
            private_json(workspace / "baseline.json", report)
            print("Complete passing baseline saved locally. Future make check runs include the local performance gate.")
        return 0
    except Exception:
        private_json(database.root / "runner-error-private.json", {"traceback": traceback.format_exc()})
        raise
    finally:
        database.stop()


if __name__ == "__main__":
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        print("Local performance run interrupted; owned services stopped.", file=sys.stderr)
        sys.exit(130)
    except Exception as error:
        # Command/driver exceptions can contain a dump path or URI. Keep only a
        # finite category in terminal/automation output; detailed logs stay local.
        print("Local performance check failed (" + type(error).__name__ + "). Inspect the private workspace diagnostics.", file=sys.stderr)
        sys.exit(1)

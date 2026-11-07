import unittest
import tempfile
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from run import go_benchmarks, parse_go_benchmarks, REQUIRED_BENCHMARKS


class GoBenchmarkOutput(unittest.TestCase):
    def test_failed_operation_stops_before_missing_measurements_can_be_read(self):
        with tempfile.TemporaryDirectory() as directory:
            database = SimpleNamespace(root=Path(directory), uri=lambda *args: "synthetic-uri")
            with patch("run.invoke", return_value=1):
                with self.assertRaisesRegex(RuntimeError, "Synthetic operation checks failed"):
                    go_benchmarks(database, 3)

    def test_warm_profile_is_mandatory_and_replaces_cold_account_samples(self):
        with tempfile.TemporaryDirectory() as directory:
            database = SimpleNamespace(root=Path(directory), uri=lambda *args: "synthetic-uri")
            lines = []
            for name in sorted(REQUIRED_BENCHMARKS):
                package, benchmark = name.removeprefix("synthetic.").split(".", 1)
                lines.append("pkg: example/internal/" + package)
                lines.extend(benchmark + "-2 1 6000 ns/op 2400 B/op 43 allocs/op" for _ in range(3))
            (database.root / "synthetic.log").write_text("\n".join(lines))
            (database.root / "synthetic-books.log").write_text(
                "pkg: example/internal/books\n" + "BenchmarkBookRelease-2 100 6000 ns/op 2400 B/op 43 allocs/op\n" * 3
            )
            (database.root / "synthetic-accounts.log").write_text("")
            with patch("run.invoke", return_value=0):
                with self.assertRaisesRegex(RuntimeError, "Warm benchmark profile"):
                    go_benchmarks(database, 3)
                accounts = [name for name in REQUIRED_BENCHMARKS if ".BenchmarkProviderAccountValidation/" in name]
                warm = ["pkg: example/internal/aiosource"]
                for name in accounts:
                    benchmark = name.split(".", 2)[2]
                    warm.extend(benchmark + "-2 100 3000 ns/op 1200 B/op 21 allocs/op" for _ in range(3))
                (database.root / "synthetic-accounts.log").write_text("\n".join(warm))
                measured = {row["id"]: row for row in go_benchmarks(database, 3)}
                self.assertEqual(set(measured), REQUIRED_BENCHMARKS)
                for name in accounts:
                    self.assertEqual(measured[name]["samples_ms"], [0.003] * 3)
                    self.assertEqual(measured[name]["bytes_per_op"], 1200)

    def test_microbenchmark_metrics_remain_per_operation(self):
        groups, allocations = parse_go_benchmarks(
            "pkg: example/internal/books\n"
            "BenchmarkParse-2     100 6000 ns/op 2400 B/op 43 allocs/op\n"
        )
        self.assertEqual(groups["synthetic.books.BenchmarkParse"], [0.006])
        self.assertEqual(allocations["synthetic.books.BenchmarkParse"], [(2400, 43)])

    def test_intervening_background_logs_preserve_each_complete_sample(self):
        groups, allocations = parse_go_benchmarks(
            "pkg: example/internal/store\n"
            "BenchmarkRead/list-2     1 2500000 ns/op 1000 B/op 10 allocs/op\n"
            "BenchmarkRead/list-2     background maintenance log\n"
            "another background message\n"
            "       1 3500000 ns/op 2000 B/op 20 allocs/op\n"
        )
        self.assertEqual(groups["synthetic.store.BenchmarkRead/list"], [2.5, 3.5])
        self.assertEqual(allocations["synthetic.store.BenchmarkRead/list"], [(1000, 10), (2000, 20)])

    def test_result_without_its_package_and_header_is_not_fabricated(self):
        groups, _ = parse_go_benchmarks(
            "pkg: example/internal/store\n"
            "       1 2500000 ns/op\n"
            "BenchmarkMissing-2 background log\n"
        )
        self.assertEqual(dict(groups), {})


if __name__ == "__main__":
    unittest.main()

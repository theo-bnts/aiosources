import unittest

from run import parse_go_benchmarks


class GoBenchmarkOutput(unittest.TestCase):
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

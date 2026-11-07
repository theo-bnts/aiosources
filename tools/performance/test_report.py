import copy
import unittest

from report import evaluate, summarize


def report(samples=(100, 101, 99, 100, 100)):
    return {"schema": 1, "suite": 1, "dataset": "synthetic-dataset-hash", "environment": {"cpu": 2},
            "resources": {"peak_rss_mib": 100, "sampled_cpu_seconds": 2},
            "cases": [summarize({"id": "api.search.book", "samples_ms": list(samples), "budget_ms": 5000, "contract": "same-results"})]}


class PerformanceContracts(unittest.TestCase):
    def test_warm_samples_report_median_and_tail(self):
        row = report((1, 2, 5, 8, 10))["cases"][0]
        self.assertEqual(row["median_ms"], 5)
        self.assertEqual(row["p95_ms"], 10)

    def test_speed_does_not_excuse_missing_or_different_results(self):
        before = report()
        after = report((1, 1, 1, 1, 1))
        after["cases"][0]["contract"] = "different-results"
        self.assertEqual(evaluate(after, before)[0]["reason"], "result_changed")

    def test_stable_timings_and_small_noise_pass(self):
        self.assertEqual(evaluate(report((110, 101, 120, 100, 102)), report()), [])

    def test_material_slowdown_is_detected(self):
        reasons = {row["reason"] for row in evaluate(report((170, 180, 170, 180, 170)), report())}
        self.assertIn("median_ms_regression", reasons)
        self.assertIn("p95_ms_regression", reasons)

    def test_absolute_deadline_applies_without_any_baseline(self):
        failures = evaluate(report((6000,) * 5))
        self.assertEqual(failures[0]["reason"], "time_budget")

    def test_missing_case_is_not_silently_skipped(self):
        after = report()
        after["cases"] = []
        with self.assertRaisesRegex(ValueError, "coverage"):
            evaluate(after, report())

    def test_failed_baselines_and_tampered_summaries_are_rejected(self):
        for baseline in (report((6000,) * 5), report()):
            if baseline["cases"][0]["p95_ms"] < 5000:
                baseline["failures"] = [{"reason": "empty_result"}]
            with self.assertRaisesRegex(ValueError, "baseline"):
                evaluate(report(), baseline)
        after = report()
        after["cases"][0]["median_ms"] = 0
        with self.assertRaisesRegex(ValueError, "summaries"):
            evaluate(after)

    def test_changed_coverage_and_invalid_budgets_cannot_pass(self):
        after = report()
        after["cases"][0]["id"] = "api.other"
        with self.assertRaisesRegex(ValueError, "coverage changed"):
            evaluate(after, report())
        for budget in (0, -1, float("nan"), float("inf")):
            after = report()
            after["cases"][0]["budget_ms"] = budget
            with self.assertRaisesRegex(ValueError, "budget"):
                evaluate(after)

    def test_different_dump_or_machine_cannot_claim_a_speedup(self):
        for key in ("schema", "suite", "dataset", "environment"):
            after = report()
            after[key] = "different"
            with self.assertRaisesRegex(ValueError, key):
                evaluate(after, report())

    def test_operation_failures_are_finite_and_visible(self):
        after = report()
        after["cases"][0]["error"] = "empty_result"
        self.assertEqual(evaluate(after)[0], {"case": "api.search.book", "reason": "empty_result"})

    def test_duplicate_ids_and_invalid_samples_are_rejected(self):
        after = report()
        after["cases"] *= 2
        with self.assertRaisesRegex(ValueError, "Duplicate"):
            evaluate(after)
        for values in ([], [float("nan")], [float("inf")], [-1]):
            with self.assertRaisesRegex(ValueError, "Invalid"):
                summarize({"samples_ms": values})

    def test_memory_and_allocations_are_compared(self):
        before = report()
        before["cases"][0].update(bytes_per_op=10000, allocs_per_op=100)
        after = copy.deepcopy(before)
        after["cases"][0].update(bytes_per_op=20000, allocs_per_op=200)
        after["resources"] = {"peak_rss_mib": 300, "sampled_cpu_seconds": 10}
        reasons = {row["reason"] for row in evaluate(after, before)}
        self.assertEqual(reasons, {"bytes_per_op_regression", "allocs_per_op_regression", "peak_rss_mib_regression", "sampled_cpu_seconds_regression"})

    def test_rss_uses_a_conservative_absolute_ceiling_and_relative_guard(self):
        before = report()
        before["resources"]["peak_rss_mib"] = 1950
        after = copy.deepcopy(before)
        after["resources"]["peak_rss_mib"] = 2200
        self.assertEqual(evaluate(after, before), [])
        after["resources"]["peak_rss_mib"] = 2500
        self.assertIn("peak_rss_mib_regression", {row["reason"] for row in evaluate(after, before)})
        after["resources"]["peak_rss_mib"] = 4097
        self.assertIn("memory_budget", {row["reason"] for row in evaluate(after)})


if __name__ == "__main__":
    unittest.main()

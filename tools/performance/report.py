"""Local performance contracts. Reports contain case IDs and numbers, never payloads."""

import math
import statistics

SCHEMA = 1


def percentile(values, fraction):
    ordered = sorted(values)
    return ordered[max(0, math.ceil(len(ordered) * fraction) - 1)]


def summarize(case):
    values = case["samples_ms"]
    if not values or any(not isinstance(v, (int, float)) or not math.isfinite(v) or v < 0 for v in values):
        raise ValueError("Invalid performance samples")
    return {**case, "median_ms": round(statistics.median(values), 3), "p95_ms": round(percentile(values, .95), 3)}


def evaluate(report, baseline=None):
    """Absolute budgets always apply. Comparing incompatible datasets fails closed."""
    failures = []
    cases = report["cases"]
    if not cases:
        raise ValueError("Performance coverage is empty")
    ids = [case["id"] for case in cases]
    if len(ids) != len(set(ids)):
        raise ValueError("Duplicate performance case IDs")
    for case in cases:
        # Validate stored reports as well as freshly collected measurements.
        validated = summarize(case)
        if any(validated[key] != case[key] for key in ("median_ms", "p95_ms")):
            raise ValueError("Stored performance summaries do not match their samples")
        if not isinstance(case["budget_ms"], (float, int)) or not math.isfinite(case["budget_ms"]) or case["budget_ms"] <= 0:
            raise ValueError("Invalid performance budget")
        if case.get("error"):
            failures.append({"case": case["id"], "reason": case["error"]})
        elif max(case["samples_ms"]) > case["budget_ms"]:
            failures.append({"case": case["id"], "reason": "time_budget", "actual_ms": max(case["samples_ms"]), "limit_ms": case["budget_ms"]})
    resources = report.get("resources", {})
    # Summed RSS includes PostgreSQL shared mappings in several processes.
    # Use a conservative ceiling; the baseline still guards a 20% increase.
    if resources.get("peak_rss_mib", 0) > 4096:
        failures.append({"case": "resources", "reason": "memory_budget"})
    if not baseline:
        return failures
    if baseline.get("failures") or evaluate(baseline):
        raise ValueError("A failed or incomplete run cannot serve as the baseline")
    for key in ("schema", "dataset", "environment", "suite"):
        if report[key] != baseline[key]:
            raise ValueError("Baseline has a different " + key)
    old = {case["id"]: case for case in baseline["cases"]}
    if set(ids) != set(old):
        raise ValueError("Performance coverage changed; review and establish a complete baseline")
    for case in cases:
        before = old[case["id"]]
        if case.get("error") or before.get("error"):
            continue
        if case.get("contract") != before.get("contract"):
            failures.append({"case": case["id"], "reason": "result_changed"})
        # Both a relative and an absolute difference are required. Tiny control
        # timings and scheduler noise must not masquerade as regressions.
        for metric in ("median_ms", "p95_ms"):
            limit = max(before[metric] * 1.20, before[metric] + case.get("noise_ms", 50))
            if case[metric] > limit:
                failures.append({"case": case["id"], "reason": metric + "_regression", "actual_ms": case[metric], "limit_ms": round(limit, 3)})
        for metric, slack in (("bytes_per_op", 4096), ("allocs_per_op", 10)):
            if metric in case and metric in before and case[metric] > max(before[metric] * 1.20, before[metric] + slack):
                failures.append({"case": case["id"], "reason": metric + "_regression"})
    for metric, slack in (("peak_rss_mib", 128), ("sampled_cpu_seconds", 2)):
        before = baseline.get("resources", {}).get(metric)
        after = resources.get(metric)
        if before is not None and after is not None and after > max(before * 1.20, before + slack):
            failures.append({"case": "resources", "reason": metric + "_regression"})
    return failures

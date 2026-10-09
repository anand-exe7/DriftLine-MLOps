"""
Replays real loan applications through the Driftline prediction proxy.

    python scripts/simulate_traffic.py                          # normal traffic, 60s @ 20 req/s
    python scripts/simulate_traffic.py --mode drift --check     # shifted traffic, then run a drift check
    python scripts/simulate_traffic.py --model loan_default_lr --rps 50 --duration 120

Normal mode samples rows from the dataset as-is, so live inputs match the
training distribution. Drift mode applies a plausible downturn to the same
rows (lower incomes, higher rates, weaker credit, more unemployment), which
the drift engine should flag on its next pass (or immediately with --check).
"""
import argparse
import random
import sys
import threading
import time
from collections import Counter
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

import pandas as pd

import driftline_api as api

DATA = Path(__file__).resolve().parents[1] / "ml" / "projects" / "loan_default" / "data" / "Loan_default.csv"
BINARY = {"HasMortgage", "HasDependents", "HasCoSigner"}


def drift(row: dict, rng: random.Random) -> dict:
    """An economic downturn, applied to one application."""
    row = dict(row)
    row["Income"] = max(15000, row["Income"] * rng.uniform(0.45, 0.7))
    row["InterestRate"] = min(25.0, row["InterestRate"] + rng.uniform(4, 9))
    row["CreditScore"] = max(300, row["CreditScore"] - rng.randint(60, 180))
    row["DTIRatio"] = min(0.9, row["DTIRatio"] + rng.uniform(0.1, 0.3))
    if rng.random() < 0.35:
        row["EmploymentType"] = "Unemployed"
    return row


def encode(row: dict, features: list[str]) -> dict:
    """Same encoding as training: Yes/No -> 1/0, one-hot with first level dropped."""
    out = {}
    for name in features:
        if name in row:
            v = row[name]
            out[name] = (1.0 if v == "Yes" else 0.0) if name in BINARY else float(v)
        else:
            col, level = name.split("_", 1)
            out[name] = 1.0 if row.get(col) == level else 0.0
    return out


def feature_list(model: str) -> list[str]:
    versions = api.get(f"/models/{model}/versions")
    serving = next((v for v in versions if v["stage"] == "production"), versions[0])
    return serving["feature_schema"]["features"]


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--model", default="loan_default_xgb")
    ap.add_argument("--mode", choices=["normal", "drift"], default="normal")
    ap.add_argument("--rps", type=float, default=20, help="target requests per second")
    ap.add_argument("--duration", type=float, default=60, help="seconds to run")
    ap.add_argument("--workers", type=int, default=8)
    ap.add_argument("--check", action="store_true", help="trigger a drift check when done and print it")
    ap.add_argument("--seed", type=int, default=7)
    args = ap.parse_args()

    rng = random.Random(args.seed)
    rows = pd.read_csv(DATA).drop(columns=["LoanID", "Default"]).sample(n=20000, random_state=args.seed).to_dict("records")
    features = feature_list(args.model)
    print(f"{args.mode} traffic -> {args.model} at ~{args.rps:g} req/s for {args.duration:g}s")

    stats = Counter()
    lock = threading.Lock()

    def send(i: int):
        row = rows[i % len(rows)]
        if args.mode == "drift":
            row = drift(row, rng)
        try:
            res = api.post(f"/predict/{args.model}", {"features": encode(row, features)})
            with lock:
                stats["ok"] += 1
                stats[f"{res['version']} ({res['role']})"] += 1
                stats["flagged"] += res["prediction"]
        except api.ApiError as err:
            with lock:
                stats["error"] += 1
                stats[f"HTTP {err.status}"] += 1

    interval = 1.0 / args.rps
    deadline = time.monotonic() + args.duration
    next_report = time.monotonic() + 5
    i = 0
    with ThreadPoolExecutor(args.workers) as pool:
        start = time.monotonic()
        while time.monotonic() < deadline:
            pool.submit(send, i)
            i += 1
            # pace by schedule, not by sleep(interval), so slow requests don't lower the rate
            time.sleep(max(0.0, start + i * interval - time.monotonic()))
            if time.monotonic() >= next_report:
                with lock:
                    ok = stats["ok"]
                    print(f"  {i:>6} sent  {ok:>6} ok  {stats['error']:>4} errors  "
                          f"{(stats['flagged'] / ok * 100 if ok else 0):5.1f}% flagged as default", flush=True)
                next_report += 5

    print("\nrouting:")
    for k, v in sorted(stats.items()):
        if "(" in k or k.startswith("HTTP"):
            print(f"  {k:<28} {v}")

    if args.check:
        print("\nrunning a drift check now...")
        for r in api.post("/drift/run"):
            flag = "DRIFT" if r["drift_detected"] else "ok"
            top = sorted(r["feature_scores"].items(), key=lambda kv: kv[1]["psi"], reverse=True)[:3]
            tops = ", ".join(f"{k} {v['psi']:.2f}" for k, v in top)
            print(f"  {r['model_name']}/{r['version']:<6} n={r['sample_size']:<5} PSI {r['overall_psi']:.3f}  {flag:<5}  top: {tops}")


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        sys.exit(130)

#!/usr/bin/env python3
"""Run two alternating off/on pairs on the real sample topology; emit raw reports.

This is a small local comparison, not a statistical benchmark. Requires Docker,
Python 3 and the same prerequisites as experiment.sh. No third-party packages.
"""
import json
import os
from pathlib import Path
import signal
import subprocess
import sys


def main():
    root = Path(__file__).resolve().parent.parent
    runs = []
    for pair in range(2):
        for mode in ("off", "jaeger"):
            command = [str(root / "scripts/experiment.sh")]
            if mode == "jaeger":
                command.append("--tracing")
            command.extend(["-samples", "50", "-latency", "80ms"])
            print(f"pair={pair + 1} tracing={mode}", file=sys.stderr)
            process = subprocess.Popen(command, cwd=root, stdout=subprocess.PIPE, text=True, start_new_session=True)
            try:
                stdout, _ = process.communicate(timeout=300)
            except BaseException:
                # Signal only our subprocess group, including its Compose CLI,
                # so the wrapper's trap can tear down its exact project.
                if process.poll() is None:
                    os.killpg(process.pid, signal.SIGTERM)
                    process.communicate(timeout=30)
                raise
            if process.returncode:
                raise RuntimeError(f"experiment failed: {process.returncode}")
            report = json.loads(stdout)
            if report["status"] != "complete" or not report["cleanup_complete"]:
                raise RuntimeError("incomplete experiment or proxy cleanup")
            runs.append({"pair": pair + 1, "mode": mode, "report": report})
    deltas = []
    for pair in range(2):
        off, on = (runs[2 * pair + i]["report"] for i in (0, 1))
        deltas.append({
            "pair": pair + 1,
            "enabled_minus_disabled_mean_ms": {
                phase: on[phase]["summary"]["mean_ms"] - off[phase]["summary"]["mean_ms"]
                for phase in ("baseline", "fault", "recovery")
            },
        })
    json.dump({"schema_version": 1, "method": "two sequential off/on pairs, 50 requests/phase, 80ms fault", "runs": runs, "comparisons": deltas}, sys.stdout, indent=2)
    print()


if __name__ == "__main__":
    main()

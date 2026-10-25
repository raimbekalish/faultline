#!/usr/bin/env python3
"""Build private candidate archives, never tags, uploads or GitHub Releases."""
import argparse
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]
TARGETS = ("darwin/arm64", "darwin/amd64", "linux/arm64", "linux/amd64")


def run(*args, **kwargs):
    return subprocess.check_output(args, cwd=ROOT, text=True, **kwargs).strip()


def archive(path, files, epoch):
    """Known regular byte payloads only; stable order, modes, ownership and gzip."""
    with path.open("xb") as raw:
        with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=epoch) as gz:
            with tarfile.open(fileobj=gz, mode="w", format=tarfile.USTAR_FORMAT) as tar:
                for name, (data, mode) in sorted(files.items()):
                    if name.startswith("/") or ".." in name.split("/"):
                        raise ValueError("unsafe package path")
                    info = tarfile.TarInfo(name)
                    info.size, info.mode, info.mtime = len(data), mode, epoch
                    tar.addfile(info, io.BytesIO(data))


def publish(stage, output):
    # Exclusive directory creation and hard links never replace earlier packages.
    output.mkdir()
    created = []
    try:
        for src in sorted(stage.iterdir()):
            dest = output / src.name
            os.link(src, dest)
            created.append(dest)
    except BaseException:
        for dest in created:
            dest.unlink()
        output.rmdir()
        raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True, help="candidate label, e.g. 0.1.0-rc.1")
    parser.add_argument("--out", required=True, type=Path, help="new directory under artifacts/")
    parser.add_argument("--local", action="store_true", help="also package native CGO CLI/dashboard and matching dashboard assets")
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[a-z0-9]+(?:[.-][a-z0-9]+)*)?", args.version):
        parser.error("invalid version label")
    output = args.out.resolve()
    if not output.is_relative_to(ROOT / "artifacts") or output == ROOT / "artifacts":
        parser.error("output must be a new directory under ignored artifacts/")
    if output.exists():
        parser.error("output already exists; candidate packages are never replaced")
    if run("git", "status", "--porcelain"):
        parser.error("commit source changes first; candidates require a clean worktree")
    revision = run("git", "rev-parse", "HEAD")
    epoch = int(run("git", "show", "-s", "--format=%ct", "HEAD"))
    go = os.environ.get("FAULTLINE_GO", "go")
    host = run(go, "env", "GOHOSTOS") + "/" + run(go, "env", "GOHOSTARCH")
    if args.local and host not in TARGETS:
        parser.error("native package target is outside the evaluated platform matrix")
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".candidate-", dir=output.parent) as tmp:
        tmp = Path(tmp)
        stage = tmp / "packages"
        stage.mkdir()
        def build(command, target, cgo):
            dest = tmp / (command + "-" + target.replace("/", "-"))
            osname, arch = target.split("/")
            env = dict(os.environ, GOOS=osname, GOARCH=arch, CGO_ENABLED=str(cgo))
            flags = f"-s -w -buildid= -X github.com/raimbekalish/faultline/internal/buildinfo.Version={args.version} -X github.com/raimbekalish/faultline/internal/buildinfo.Revision={revision}"
            if command == "faultline":
                flags += f" -X main.revision={revision}"
            subprocess.run([go, "build", "-buildvcs=false", "-trimpath", "-ldflags=" + flags, "-o", str(dest), "./cmd/" + command], cwd=ROOT, env=env, check=True)
            return dest.read_bytes()
        def package(component, target, binaries, assets=False):
            metadata = {"schema_version": 1, "component": component, "version": args.version, "revision": revision, "source_epoch": epoch, "target": target, "go_toolchain": run(go, "version"), "cgo": component == "faultline-local", "frontend_lock_sha256": hashlib.sha256((ROOT / "web/package-lock.json").read_bytes()).hexdigest() if assets else None}
            files = {name: (data, 0o755) for name, data in binaries.items()}
            files["LICENSE"] = ((ROOT / "LICENSE").read_bytes(), 0o644)
            files["VERSION.json"] = ((json.dumps(metadata, sort_keys=True, indent=2) + "\n").encode(), 0o644)
            files["USAGE.md"] = ((ROOT / "docs/package-usage.md").read_bytes(), 0o644)
            if assets:
                dist = ROOT / "web/dist"
                if not (dist / "index.html").is_file():
                    raise ValueError("build web/dist first: make frontend-check")
                for path in sorted(dist.rglob("*")):
                    if path.is_symlink():
                        raise ValueError("symlink in assets")
                    if path.is_file():
                        files["web/dist/" + path.relative_to(dist).as_posix()] = (path.read_bytes(), 0o644)
                for name in ("latency.json", "timeouts.json"):
                    files["examples/" + name] = ((ROOT / "examples" / name).read_bytes(), 0o644)
            files["SHA256SUMS"] = (("".join(f"{hashlib.sha256(data).hexdigest()}  {name}\n" for name, (data, _) in sorted(files.items()))).encode(), 0o644)
            name = f"{component}_{args.version}_{revision[:12]}_{target.replace('/', '_')}.tar.gz"
            archive(stage / name, files, epoch)
        offline = {}
        for target in TARGETS:
            offline[target] = build("faultline-bundle", target, 0)
            package("faultline-bundle", target, {"faultline-bundle": offline[target]})
        if args.local:
            # Build matching assets from the committed lockfile, never stale dist/.
            subprocess.run(["npm", "--prefix", "web", "ci", "--ignore-scripts"], cwd=ROOT, check=True)
            subprocess.run(["npm", "--prefix", "web", "run", "build"], cwd=ROOT, check=True)
            package("faultline-local", host, {"faultline-bundle": offline[host], "faultline": build("faultline", host, 1), "dashboard": build("dashboard", host, 1)}, assets=True)
        sums = "".join(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n" for path in sorted(stage.iterdir()))
        (stage / "SHA256SUMS").write_text(sums)
        publish(stage, output)
    print(output)


if __name__ == "__main__":
    main()

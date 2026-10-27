#!/usr/bin/env python3
"""Build versioned GoTun artifacts; release creation stays in the manual workflow."""
import argparse
import json
import hashlib
import os
import base64
import secrets
import tempfile
import tarfile
import zipfile
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PLATFORMS = [(os, arch) for os in ("linux", "darwin", "windows") for arch in ("amd64", "arm64")]
SIGNING_SECRETS = ("MACOS_CERTIFICATE_P12", "MACOS_CERTIFICATE_PASSWORD", "MACOS_SIGNING_IDENTITY", "APPLE_ID", "APPLE_TEAM_ID", "APPLE_APP_PASSWORD")


def resolve_version(requested=None, root=ROOT):
    if requested is None:
        tag = subprocess.run(["git", "describe", "--tags", "--exact-match", "HEAD"], cwd=root, capture_output=True, text=True)
        clean = subprocess.run(["git", "status", "--porcelain", "--untracked-files=normal"], cwd=root, capture_output=True, text=True)
        requested = tag.stdout.strip() if tag.returncode == 0 and clean.returncode == 0 and not clean.stdout.strip() else "dev"
    if requested == "dev":
        return requested
    if not re.fullmatch(r"v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)", requested):
        raise ValueError("VERSION must be dev or a stable version such as v1.0.0")
    return "v" + requested.removeprefix("v")


def expected_assets(version):
    version = resolve_version(version)
    if version == "dev":
        raise ValueError("Release artifacts require VERSION=vX.Y.Z")
    assets = {f"gotun_{version}_{os}_{arch}." + ("zip" if os == "windows" else "tar.gz") for os, arch in PLATFORMS}
    return assets | {f"gotun-gui_{version}_darwin_universal.zip", f"gotun-gui_{version}_windows_amd64.zip"}


def write_manifest(directory, version):
    expected = expected_assets(version)
    actual = {p.name for p in directory.iterdir() if p.is_file() and p.name != "SHA256SUMS"}
    if actual != expected:
        raise ValueError(f"Incomplete release assets; missing={sorted(expected-actual)}, unexpected={sorted(actual-expected)}")
    lines = [f"{hashlib.sha256((directory / name).read_bytes()).hexdigest()}  {name}\n" for name in sorted(expected)]
    (directory / "SHA256SUMS").write_text("".join(lines))


def build_gui(version, platform, root=ROOT):
    version = resolve_version(version)
    config = root / "gui/wails.json"
    original = None
    lock = root / "gui/.wails-version-lock"
    lock.mkdir()  # Concurrent Wails builds must not restore each other's version.
    try:
        original = config.read_bytes()
        data = json.loads(original)
        data["info"]["productVersion"] = "0.0.0" if version == "dev" else version[1:]
        config.write_text(json.dumps(data, indent=2) + "\n")
        env = os.environ.copy()
        if platform.startswith("darwin/"):
            env["MACOSX_DEPLOYMENT_TARGET"] = "13.0"
            for name in ("CGO_CFLAGS", "CGO_LDFLAGS"):
                env[name] = re.sub(r"-mmacosx-version-min=\S+", "", env.get(name, "")) + " -mmacosx-version-min=13.0"
        subprocess.run(["wails", "build", "-s", "-skipbindings", "-m", "-nosyncgomod", "-platform", platform, "-ldflags", f"-X main.Version={version}"], cwd=root / "gui", env=env, check=True)
    finally:
        if original is not None:
            config.write_bytes(original)
        lock.rmdir()


def check_signing(env=None):
    env = os.environ if env is None else env
    missing = [name for name in SIGNING_SECRETS if not env.get(name)]
    if missing:
        raise ValueError("Signed macOS release requires secrets: " + ", ".join(missing))


def build_cli(version, directory, root=ROOT):
    version = resolve_version(version)
    expected_assets(version)
    directory.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory() as temporary:
        for system, arch in PLATFORMS:
            binary = Path(temporary) / ("gotun.exe" if system == "windows" else "gotun")
            env = dict(os.environ, GOOS=system, GOARCH=arch, CGO_ENABLED="0")
            subprocess.run(["go", "build", "-trimpath", "-ldflags", f"-s -w -X main.Version={version}", "-o", str(binary), "./cmd/gotun"], cwd=root, env=env, check=True)
            name = f"gotun_{version}_{system}_{arch}"
            if system == "windows":
                with zipfile.ZipFile(directory / (name + ".zip"), "w", zipfile.ZIP_DEFLATED) as archive:
                    archive.write(binary, binary.name)
                    archive.write(root / "LICENSE", "LICENSE")
            else:
                with tarfile.open(directory / (name + ".tar.gz"), "w:gz") as archive:
                    archive.add(binary, arcname=binary.name)
                    archive.add(root / "LICENSE", arcname="LICENSE")


def sign_mac(app, archive, env=None):
    env = os.environ if env is None else env
    check_signing(env)
    license_file = app / "Contents/Resources/LICENSE"
    license_file.parent.mkdir(parents=True, exist_ok=True)
    license_file.write_bytes((ROOT / "LICENSE").read_bytes())
    with tempfile.TemporaryDirectory() as directory:
        temporary = Path(directory)
        certificate = temporary / "developer.p12"
        certificate.write_bytes(base64.b64decode(env["MACOS_CERTIFICATE_P12"], validate=True))
        certificate.chmod(0o600)
        keychain = temporary / "release.keychain-db"
        password = secrets.token_urlsafe(32)
        def run(*args, **kwargs):
            return subprocess.run(list(args), check=True, **kwargs)
        run("security", "create-keychain", "-p", password, str(keychain))
        try:
            run("security", "set-keychain-settings", "-lut", "21600", str(keychain))
            run("security", "unlock-keychain", "-p", password, str(keychain))
            run("security", "import", str(certificate), "-k", str(keychain), "-P", env["MACOS_CERTIFICATE_PASSWORD"], "-T", "/usr/bin/codesign")
            run("security", "set-key-partition-list", "-S", "apple-tool:,apple:", "-s", "-k", password, str(keychain))
            run("codesign", "--force", "--options", "runtime", "--timestamp", "--sign", env["MACOS_SIGNING_IDENTITY"], "--keychain", str(keychain), str(app))
            run("codesign", "--verify", "--deep", "--strict", str(app))
            submission = temporary / "notarization.zip"
            run("ditto", "-c", "-k", "--keepParent", str(app), str(submission))
            result = run("xcrun", "notarytool", "submit", str(submission), "--apple-id", env["APPLE_ID"], "--password", env["APPLE_APP_PASSWORD"], "--team-id", env["APPLE_TEAM_ID"], "--wait", "--timeout", "20m", "--output-format", "json", capture_output=True, text=True)
            if json.loads(result.stdout).get("status") != "Accepted":
                raise ValueError("Apple notarization was not Accepted; no release archive was created")
            run("xcrun", "stapler", "staple", str(app))
            run("xcrun", "stapler", "validate", str(app))
            run("spctl", "--assess", "--type", "execute", str(app))
            archive.parent.mkdir(parents=True, exist_ok=True)
            run("ditto", "-c", "-k", "--keepParent", str(app), str(archive))
        finally:
            run("security", "delete-keychain", str(keychain))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["version", "build-cli", "build-gui", "manifest", "check-signing", "sign-mac", "pack-windows"])
    parser.add_argument("--version")
    parser.add_argument("--platform")
    parser.add_argument("--out", type=Path, default=ROOT / "build/release")
    parser.add_argument("--app", type=Path, default=ROOT / "gui/build/bin/GoTun.app")
    args = parser.parse_args()
    try:
        if args.command == "check-signing":
            check_signing()
            return
        version = resolve_version(args.version)
        if args.command == "version":
            print(version)
        elif args.command == "build-cli":
            build_cli(version, args.out.resolve())
        elif args.command == "build-gui":
            if not args.platform:
                parser.error("build-gui requires --platform")
            build_gui(version, args.platform)
        elif args.command == "manifest":
            write_manifest(args.out, version)
        elif args.command == "sign-mac":
            expected_assets(version)
            sign_mac(args.app, args.out / f"gotun-gui_{version}_darwin_universal.zip")
        elif args.command == "pack-windows":
            expected_assets(version)
            args.out.mkdir(parents=True, exist_ok=True)
            with zipfile.ZipFile(args.out / f"gotun-gui_{version}_windows_amd64.zip", "w", zipfile.ZIP_DEFLATED) as archive:
                archive.write(ROOT / "gui/build/bin/gotun.exe", "GoTun.exe")
                archive.write(ROOT / "LICENSE", "LICENSE")
    except subprocess.CalledProcessError as error:
        parser.exit(1, f"Release tool failed (exit {error.returncode}); no public release was created.\n")
    except (ValueError, OSError) as error:
        parser.exit(1, str(error) + "\n")


if __name__ == "__main__":
    main()

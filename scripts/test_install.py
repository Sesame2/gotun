import hashlib
import io
import json
import os
import subprocess
import sys
import tarfile
import tempfile
import unittest
from pathlib import Path

INSTALLER = Path(__file__).with_name("install.sh").resolve()


class InstallerTests(unittest.TestCase):
    def install_fixture(self, checksum=True, valid=True, shell="/bin/sh", legacy=False, compact=False, protected=False, version="v1.2.3", pin_fixture=False):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            tools, assets, target = root / "tools", root / "assets", root / "installed"
            tools.mkdir(); assets.mkdir()
            name = f"gotun_{version}_linux_amd64.tar.gz"
            binary = b"#!/bin/sh\nprintf 'fixture v1.2.3\\n'\n"
            with tarfile.open(assets / name, "w:gz") as archive:
                info = tarfile.TarInfo(f"gotun_{version}_linux_amd64" if legacy else "gotun"); info.size = len(binary); info.mode = 0o755
                archive.addfile(info, io.BytesIO(binary))
            if checksum:
                digest = hashlib.sha256((assets / name).read_bytes()).hexdigest() if valid else "0" * 64
                (assets / "SHA256SUMS").write_text(f"{digest}  {name}\n")
            (assets / "latest").write_text(json.dumps({"tag_name": "v9.9.9"}))
            (assets / "releases").write_text(json.dumps([
                {"tag_name": "v9.9.9", "assets": [{"browser_download_url": "https://github.com/Sesame2/gotun/releases/download/v9.9.9/gotun-gui_v9.9.9_darwin_universal.zip"}]},
                {"tag_name": version, "assets": [{"browser_download_url": f"https://github.com/Sesame2/gotun/releases/download/{version}/{name}"}]},
                {"tag_name": "v0.7.0", "assets": [{"browser_download_url": "https://github.com/Sesame2/gotun/releases/download/v0.7.0/gotun_v0.7.0_linux_amd64.tar.gz"}]},
            ], indent=None if compact else 2))
            (tools / "uname").write_text('#!/bin/sh\nif [ "$1" = "-s" ]; then echo Linux; else echo x86_64; fi\n')
            (tools / "curl").write_text(f'''#!{sys.executable}
import os,sys
from pathlib import Path
args=sys.argv[1:]
url=next(a for a in args if a.startswith("https://"))
name="latest" if url.endswith("/latest") else "releases" if "/releases?" in url else url.rsplit("/",1)[-1]
source=Path(os.environ["FIXTURE_ASSETS"])/name
if not source.exists(): sys.exit(22)
if "-o" in args: Path(args[args.index("-o")+1]).write_bytes(source.read_bytes())
else: sys.stdout.buffer.write(source.read_bytes())
''')
            if protected:
                (tools / "mkdir").write_text('#!/bin/sh\ncase "$*" in *"/installed") exit 1;; esac\nexec /bin/mkdir "$@"\n')
                (tools / "sudo").write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$HOME/sudo.log"\nif [ "$1" = mkdir ]; then shift; exec /bin/mkdir "$@"; fi\nexec "$@"\n')
            for file in tools.iterdir(): file.chmod(0o755)
            installer = INSTALLER
            if pin_fixture:
                # Inject the trust digest for this synthetic tar, keeping the legacy-only policy intact.
                installer = root / "install.sh"
                installer.write_text(INSTALLER.read_text().replace("5527f91342b7fe57f53fc8cfef95e39e2edd7089cdaa42696a5bbc7f016e96b9", hashlib.sha256((assets / name).read_bytes()).hexdigest()))
            env = dict(os.environ, HOME=str(root), PATH=str(tools) + os.pathsep + os.environ["PATH"], FIXTURE_ASSETS=str(assets), SHELL=shell)
            env.pop("GOTUN_VERSION", None)
            result = subprocess.run(["sh", str(installer), "--install-dir", str(target), "--no-completions"], env=env, capture_output=True, text=True)
            if protected:
                self.assertIn(f"mkdir -p {target}", (root / "sudo.log").read_text() if (root / "sudo.log").exists() else "")
            return result, (target / "gotun").read_bytes() if (target / "gotun").exists() else None

    def test_gui_only_latest_release_does_not_hide_cli_release(self):
        result, binary = self.install_fixture()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn(b"fixture v1.2.3", binary)

    def test_compact_api_response_still_selects_first_cli_release(self):
        result, binary = self.install_fixture(compact=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIsNotNone(binary)

    def test_missing_protected_install_directory_uses_sudo(self):
        result, binary = self.install_fixture(protected=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIsNotNone(binary)

    def test_only_legacy_release_can_use_pinned_checksum(self):
        result, binary = self.install_fixture(checksum=False, version="v0.8.1", legacy=True, pin_fixture=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIsNotNone(binary)
        result, binary = self.install_fixture(checksum=False, version="v0.8.1", legacy=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Checksum validation failed", result.stdout + result.stderr)
        self.assertIsNone(binary)

    def test_missing_or_wrong_checksum_never_installs_binary(self):
        for checksum, valid in [(False, False), (True, False)]:
            with self.subTest(checksum=checksum):
                result, binary = self.install_fixture(checksum, valid)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("Checksum validation failed" if checksum else "SHA256SUMS is required", result.stdout + result.stderr)
                self.assertIsNone(binary)

    def test_existing_cli_archive_name_still_installs(self):
        result, binary = self.install_fixture(legacy=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn(b"fixture v1.2.3", binary)

    def test_fish_without_completions_does_not_fail_after_install(self):
        result, binary = self.install_fixture(shell="/bin/fish")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertNotIn("unbound variable", result.stderr)
        self.assertIn(b"fixture v1.2.3", binary)


if __name__ == "__main__":
    unittest.main()

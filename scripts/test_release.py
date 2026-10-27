import importlib.util
import subprocess
import tempfile
import unittest
import json
from unittest.mock import patch
from pathlib import Path


class VersionTests(unittest.TestCase):
    def test_unversioned_build_is_dev_and_exact_clean_tag_is_release(self):
        spec = importlib.util.spec_from_file_location("release", Path(__file__).with_name("release.py"))
        release = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(release)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            def git(*args):
                return subprocess.run(["git", *args], cwd=root, check=True, capture_output=True, text=True)
            git("init")
            (root / "source.txt").write_text("original\n")
            git("add", "source.txt")
            git("-c", "user.name=Release Test", "-c", "user.email=release@example.test", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "fixture")
            self.assertEqual(release.resolve_version(root=root), "dev")
            git("tag", "v1.2.3")
            self.assertEqual(release.resolve_version(root=root), "v1.2.3")
            self.assertEqual(release.resolve_version("1.2.4", root), "v1.2.4")
            (root / "new.go").write_text("package fixture\n")
            self.assertEqual(release.resolve_version(root=root), "dev")
            (root / "new.go").unlink()
            (root / "source.txt").write_text("edited\n")
            self.assertEqual(release.resolve_version(root=root), "dev")

    def test_invalid_release_version_is_rejected(self):
        spec = importlib.util.spec_from_file_location("release", Path(__file__).with_name("release.py"))
        release = importlib.util.module_from_spec(spec); spec.loader.exec_module(release)
        for version in ("", "1.0", "../v1.0.0", "v1.0.0;invalid"):
            with self.subTest(version=version):
                with self.assertRaises(ValueError):
                    release.resolve_version(version)

    def test_manifest_requires_all_cli_and_gui_assets(self):
        spec = importlib.util.spec_from_file_location("release", Path(__file__).with_name("release.py"))
        release = importlib.util.module_from_spec(spec); spec.loader.exec_module(release)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with self.assertRaises(ValueError):
                release.write_manifest(root, "v1.2.3")
            for name in release.expected_assets("v1.2.3"):
                (root / name).write_bytes(b"abc")
            release.write_manifest(root, "v1.2.3")
            lines = (root / "SHA256SUMS").read_text().splitlines()
            self.assertEqual(len(lines), 8)
            self.assertTrue(all(line.startswith("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad  ") for line in lines))

    def test_gui_build_injects_version_and_restores_config_on_failure(self):
        spec = importlib.util.spec_from_file_location("release", Path(__file__).with_name("release.py"))
        release = importlib.util.module_from_spec(spec); spec.loader.exec_module(release)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); (root / "gui").mkdir()
            config = root / "gui/wails.json"
            original = b'{"info":{"productVersion":"0.0.0"}}\n'
            config.write_bytes(original)
            def build(command, **kwargs):
                self.assertEqual(json.loads(config.read_text())["info"]["productVersion"], "1.2.3")
                self.assertIn("-X main.Version=v1.2.3", command)
                self.assertIn("-mmacosx-version-min=13.0", kwargs["env"]["CGO_CFLAGS"])
                raise subprocess.CalledProcessError(1, command)
            with patch.object(release.subprocess, "run", side_effect=build):
                with self.assertRaises(subprocess.CalledProcessError):
                    release.build_gui("v1.2.3", "darwin/universal", root)
            self.assertEqual(config.read_bytes(), original)

    def test_gui_build_reads_config_only_after_acquiring_lock(self):
        spec = importlib.util.spec_from_file_location("release", Path(__file__).with_name("release.py"))
        release = importlib.util.module_from_spec(spec); spec.loader.exec_module(release)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); (root / "gui").mkdir()
            config = root / "gui/wails.json"
            config.write_text('{"info":{"productVersion":"another-build"}}')
            restored = b'{"info":{"productVersion":"0.0.0"}}\n'
            mkdir = Path.mkdir
            def acquire(path, *args, **kwargs):
                if path.name == ".wails-version-lock":
                    # A preceding builder restored its config just before this lock succeeds.
                    config.write_bytes(restored)
                return mkdir(path, *args, **kwargs)
            with patch.object(Path, "mkdir", acquire), patch.object(release.subprocess, "run"):
                release.build_gui("dev", "windows/amd64", root)
            self.assertEqual(config.read_bytes(), restored)

    def test_signing_fails_before_running_tools_without_credentials(self):
        spec = importlib.util.spec_from_file_location("release", Path(__file__).with_name("release.py"))
        release = importlib.util.module_from_spec(spec); spec.loader.exec_module(release)
        with patch.object(release.subprocess, "run") as run:
            with self.assertRaises(ValueError):
                release.check_signing({})
            run.assert_not_called()

    def test_notarization_rejection_never_packages_or_staples_app(self):
        spec = importlib.util.spec_from_file_location("release", Path(__file__).with_name("release.py"))
        release = importlib.util.module_from_spec(spec); spec.loader.exec_module(release)
        env = {name: "fixture" for name in release.SIGNING_SECRETS}
        env["MACOS_CERTIFICATE_P12"] = "eA=="
        with tempfile.TemporaryDirectory() as directory:
            app, archive = Path(directory) / "GoTun.app", Path(directory) / "release.zip"
            app.mkdir()
            def run(command, **kwargs):
                if command[0] == "codesign":
                    self.assertEqual((app / "Contents/Resources/LICENSE").read_bytes(), (release.ROOT / "LICENSE").read_bytes())
                return subprocess.CompletedProcess(command, 0, stdout='{"status":"Invalid"}')
            with patch.object(release.subprocess, "run", side_effect=run) as commands:
                with self.assertRaises(ValueError):
                    release.sign_mac(app, archive, env)
                self.assertFalse(any("stapler" in call.args[0] for call in commands.call_args_list))
                self.assertFalse(archive.exists())


if __name__ == "__main__":
    unittest.main()

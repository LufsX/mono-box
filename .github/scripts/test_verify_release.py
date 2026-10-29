import contextlib
import importlib.util
import io
import json
import struct
import tempfile
import unittest
import zipfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("verify_release", Path(__file__).with_name("verify-release.py"))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ReleaseVerificationTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        self.version = "v6.5"
        self.version_code = 202610290200
        self.repository = "example/mono-box"

    def fixtures(self, prerelease=False):
        tag = "Prerelease" if prerelease else self.version
        base = "https://cors.isteed.cc/https://github.com/example/mono-box/releases"
        update_base = base + ("/download/Prerelease" if prerelease else "/latest/download")
        for arch, elf_class, machine in [("arm64-v8a", 2, 183), ("armeabi-v7a", 1, 40), ("x86_64", 2, 62), ("x86", 1, 3)]:
            name = f"MonoBox_{self.version}_{arch}.zip"
            header = bytearray(64)
            header[:6] = b"\x7fELF" + bytes([elf_class, 1])
            struct.pack_into("<H", header, 18, machine)
            props = f"id=mono_box\nversion={self.version}\nversionCode={self.version_code}\nupdateJson={update_base}/update-{arch}.json\n"
            with zipfile.ZipFile(self.directory / name, "w") as package:
                package.writestr("module.prop", props)
                package.writestr(f"bin/monobox-{arch}", header)
                package.writestr("customize.sh", b"#!/system/bin/sh\nexit 0\n")
            metadata = {
                "version": self.version,
                "versionCode": self.version_code,
                "zipUrl": f"{base}/download/{tag}/{name}",
                "changelog": update_base + ("/prerelease-changelog.md" if prerelease else "/changelog.md"),
            }
            (self.directory / f"update-{arch}.json").write_text(json.dumps(metadata))
        return tag

    def verify(self, tag):
        with contextlib.redirect_stdout(io.StringIO()):
            release.verify(self.directory, self.version, self.version_code, tag, self.repository)

    def test_release_and_prerelease_for_a_fork(self):
        for prerelease in (False, True):
            self.verify(self.fixtures(prerelease))

    def test_rejects_cross_architecture_update(self):
        tag = self.fixtures()
        path = self.directory / "update-x86.json"
        metadata = json.loads(path.read_text())
        metadata["zipUrl"] = metadata["zipUrl"].replace("_x86.zip", "_arm64-v8a.zip")
        path.write_text(json.dumps(metadata))
        with self.assertRaisesRegex(ValueError, "update metadata"):
            self.verify(tag)

    def test_rejects_different_build_version_code(self):
        tag = self.fixtures()
        path = self.directory / "update-x86.json"
        metadata = json.loads(path.read_text())
        metadata["versionCode"] += 1
        path.write_text(json.dumps(metadata))
        with self.assertRaisesRegex(ValueError, "update metadata"):
            self.verify(tag)

    def test_rejects_wrong_elf_under_the_right_filename(self):
        tag = self.fixtures()
        path = self.directory / "MonoBox_v6.5_x86.zip"
        with zipfile.ZipFile(path) as package:
            props = package.read("module.prop")
        header = bytearray(64)
        header[:6] = b"\x7fELF\x02\x01"
        struct.pack_into("<H", header, 18, 183)
        with zipfile.ZipFile(path, "w") as package:
            package.writestr("module.prop", props)
            package.writestr("bin/monobox-x86", header)
        with self.assertRaisesRegex(ValueError, "ELF architecture"):
            self.verify(tag)

    def test_rejects_generic_update_url(self):
        tag = self.fixtures()
        path = self.directory / "MonoBox_v6.5_x86.zip"
        with zipfile.ZipFile(path) as package:
            props = package.read("module.prop").replace(b"update-x86.json", b"update.json")
            binary = package.read("bin/monobox-x86")
        with zipfile.ZipFile(path, "w") as package:
            package.writestr("module.prop", props)
            package.writestr("bin/monobox-x86", binary)
        with self.assertRaisesRegex(ValueError, "updateJson"):
            self.verify(tag)

    def test_rejects_damaged_installer_payload(self):
        tag = self.fixtures()
        path = self.directory / "MonoBox_v6.5_x86.zip"
        original = path.read_bytes()
        damaged = original.replace(b"#!/system/bin/sh\nexit 0\n", b"#!/system/bin/sh\nexit 1\n", 1)
        self.assertNotEqual(original, damaged)
        path.write_bytes(damaged)
        with self.assertRaisesRegex(ValueError, "damaged ZIP entry customize.sh"):
            self.verify(tag)

    def test_rejects_duplicate_package_entries(self):
        tag = self.fixtures()
        path = self.directory / "MonoBox_v6.5_x86.zip"
        with zipfile.ZipFile(path) as package:
            props = package.read("module.prop")
        with zipfile.ZipFile(path, "a") as package, self.assertWarns(UserWarning):
            package.writestr("module.prop", props)
        with self.assertRaisesRegex(ValueError, "duplicate ZIP entries"):
            self.verify(tag)

    def test_rejects_missing_architecture_and_legacy_assets(self):
        tag = self.fixtures()
        (self.directory / "update-x86.json").unlink()
        with self.assertRaisesRegex(ValueError, "missing="):
            self.verify(tag)
        self.fixtures()
        (self.directory / "update.json").write_text("{}")
        with self.assertRaisesRegex(ValueError, "unexpected="):
            self.verify(tag)


if __name__ == "__main__":
    unittest.main()

#!/usr/bin/env python3
"""Check that every update manifest and native controller matches its ABI package."""

import argparse
import json
import struct
import sys
import zipfile
from pathlib import Path

ARCHITECTURES = {
    "arm64-v8a": (2, 183),
    "armeabi-v7a": (1, 40),
    "x86_64": (2, 62),
    "x86": (1, 3),
}
URL_PREFIX = "https://cors.isteed.cc/https://github.com/"


def verify(directory, version, version_code, tag, repository):
    expected = {
        filename
        for arch in ARCHITECTURES
        for filename in (f"MonoBox_{version}_{arch}.zip", f"update-{arch}.json")
    }
    actual = {path.name for path in directory.iterdir() if path.is_file()}
    if actual != expected:
        raise ValueError(f"Release files differ: missing={sorted(expected - actual)}, unexpected={sorted(actual - expected)}")

    base = f"{URL_PREFIX}{repository}/releases"
    prerelease = tag == "Prerelease"
    update_base = f"{base}/download/Prerelease" if prerelease else f"{base}/latest/download"
    changelog = "prerelease-changelog.md" if prerelease else "changelog.md"
    for arch, (elf_class, machine) in ARCHITECTURES.items():
        zip_name = f"MonoBox_{version}_{arch}.zip"
        update_name = f"update-{arch}.json"
        metadata = json.loads((directory / update_name).read_text(encoding="utf-8"))
        expected_metadata = {
            "version": version,
            "versionCode": version_code,
            "zipUrl": f"{base}/download/{tag}/{zip_name}",
            "changelog": f"{update_base}/{changelog}",
        }
        if metadata != expected_metadata:
            raise ValueError(f"{update_name}: update metadata does not match {arch} and {tag}")

        with zipfile.ZipFile(directory / zip_name) as package:
            names = package.namelist()
            if len(names) != len(set(names)):
                raise ValueError(f"{zip_name}: duplicate ZIP entries")
            damaged = package.testzip()
            if damaged is not None:
                raise ValueError(f"{zip_name}: damaged ZIP entry {damaged}")
            binaries = {name for name in names if name.startswith("bin/") and not name.endswith("/")}
            binary_name = f"bin/monobox-{arch}"
            if binaries != {binary_name}:
                raise ValueError(f"{zip_name}: expected only {binary_name}, found {sorted(binaries)}")
            header = package.read(binary_name)[:20]
            if len(header) < 20 or header[:4] != b"\x7fELF" or header[4] != elf_class or header[5] != 1 or struct.unpack_from("<H", header, 18)[0] != machine:
                raise ValueError(f"{zip_name}: native ELF architecture does not match {arch}")

            lines = package.read("module.prop").decode("utf-8").splitlines()
            required_properties = {
                "id": "mono_box",
                "version": version,
                "versionCode": str(version_code),
                "updateJson": f"{update_base}/{update_name}",
            }
            for key, value in required_properties.items():
                matches = [line.split("=", 1)[1] for line in lines if line.startswith(key + "=")]
                if matches != [value]:
                    raise ValueError(f"{zip_name}: wrong or duplicated {key}: {matches}")
        print(f"{arch}: package, ELF, version and update URLs verified")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("directory", type=Path)
    parser.add_argument("--version", required=True)
    parser.add_argument("--version-code", type=int, required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--repository", required=True)
    args = parser.parse_args()
    try:
        verify(args.directory, args.version, args.version_code, args.tag, args.repository)
    except (OSError, ValueError, KeyError, zipfile.BadZipFile) as error:
        print(f"Release verification failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

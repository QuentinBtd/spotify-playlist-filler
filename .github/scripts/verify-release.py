#!/usr/bin/env python3
"""Verify local archives/checksums, and optionally draft assets/index readbacks."""
import argparse
import hashlib
import json
from pathlib import Path


def verify(dist, release_json=None, image_index=None, tag=None):
    metadata = json.loads((dist / "metadata.json").read_text())
    version = metadata["version"]
    expected = {
        f"spotify-playlist-filler_{version}_{os}_{arch}.{extension}"
        for os, extension in [("linux", "tar.gz"), ("darwin", "tar.gz"), ("windows", "zip")]
        for arch in ["amd64", "arm64"]
    }
    entries = {}
    for line in (dist / "checksums.txt").read_text().splitlines():
        checksum, filename = line.split(maxsplit=1)
        filename = filename.removeprefix("*")
        if Path(filename).name != filename or filename in entries:
            raise ValueError("Invalid or duplicate checksum filename")
        if len(checksum) != 64:
            raise ValueError("Not a SHA-256 checksum")
        entries[filename] = checksum
    if set(entries) != expected:
        raise ValueError(f"Expected six archives, received {sorted(entries)}")
    for filename, checksum in entries.items():
        if hashlib.sha256((dist / filename).read_bytes()).hexdigest() != checksum:
            raise ValueError(f"Checksum mismatch: {filename}")
    if release_json:
        release = json.loads(Path(release_json).read_text())
        if not release["isDraft"] or release["tagName"] != tag:
            raise ValueError("Release must still be a matching draft")
        assets = {asset["name"] for asset in release["assets"]}
        if not (expected | {"checksums.txt"}).issubset(assets):
            raise ValueError("Missing uploaded assets")
    if image_index:
        index = json.loads(Path(image_index).read_text())
        platforms = {
            (manifest.get("platform", {}).get("os"), manifest.get("platform", {}).get("architecture"))
            for manifest in index.get("manifests", [])
        }
        platforms.discard(("unknown", "unknown"))
        if platforms != {("linux", "amd64"), ("linux", "arm64")}:
            raise ValueError(f"Unexpected image platforms: {platforms}")
    print(f"Verified {len(expected)} archives and SHA-256 checksums for {version}")
    if release_json:
        print("Verified matching draft and seven required uploaded asset names")
    if image_index:
        print("Verified registry index platforms linux/amd64 and linux/arm64")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--dist", type=Path, default=Path("dist"))
    parser.add_argument("--release-json")
    parser.add_argument("--image-index")
    parser.add_argument("--tag")
    args = parser.parse_args()
    verify(args.dist, args.release_json, args.image_index, args.tag)

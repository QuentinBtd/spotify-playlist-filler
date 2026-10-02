#!/usr/bin/env python3
"""Fail closed before publication when the versioned GHCR tag already exists."""
import json
import os
import urllib.error
import urllib.request

class NoRedirect(urllib.request.HTTPRedirectHandler):
    """Fail on redirects instead of forwarding Authorization to another URL."""

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


urlopen = urllib.request.build_opener(NoRedirect()).open


def ensure_new_tag(tag, owner, package, token, bootstrap_tag=None):
    page = 1
    while True:
        url = (
            f"https://api.github.com/users/{owner}/packages/container/"
            f"{package}/versions?per_page=100&page={page}"
        )
        request = urllib.request.Request(
            url,
            headers={
                "Authorization": f"Bearer {token}",
                "Accept": "application/vnd.github+json",
                "User-Agent": "spf-release-check",
            },
        )
        try:
            with urlopen(request, timeout=30) as response:
                versions = json.load(response)
        except urllib.error.HTTPError as error:
            # A 404 can hide an existing private package: never infer absence.
            # Only an operator-approved, exact release tag can bootstrap page 1.
            if error.code == 404 and page == 1 and bootstrap_tag == tag:
                print(f"Explicit first-package bootstrap approved for {tag}; absence is NOT proven")
                return
            raise
        if not isinstance(versions, list):
            raise ValueError("Unexpected packages API response")
        for version in versions:
            tags = version.get("metadata", {}).get("container", {}).get("tags", [])
            if tag in tags:
                raise RuntimeError(
                    f"GHCR tag {tag} already exists; inspect the partial release manually, "
                    "do not overwrite a versioned image"
                )
        if len(versions) < 100:
            print(f"Versioned GHCR tag {tag} is not present")
            return
        page += 1


if __name__ == "__main__":
    ensure_new_tag(
        os.environ["RELEASE_TAG"],
        "QuentinBtd",
        "spotify-playlist-filler",
        os.environ["GH_TOKEN"],
        bootstrap_tag=os.environ.get("SPF_GHCR_BOOTSTRAP_TAG"),
    )

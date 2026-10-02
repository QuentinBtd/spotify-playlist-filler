"""Offline transport fixtures only: never contact GHCR or use real credentials."""
import importlib.util
import io
import json
from pathlib import Path
import unittest
from unittest.mock import patch
import urllib.error
import urllib.request
import urllib.response
from email.message import Message

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("guard", ROOT / "ensure-new-image-tag.py")
guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)


def response(value):
    return io.BytesIO(json.dumps(value).encode())


class GuardTests(unittest.TestCase):
    def check(self, **kwargs):
        guard.ensure_new_tag("v1.2.0", "fixture-owner", "fixture-package", "fixture-not-a-token", **kwargs)

    def test_ambiguous_404_blocks(self):
        error = urllib.error.HTTPError("https://api.github.com/fixture", 404, "inaccessible or missing", {}, None)
        with patch.object(guard, "urlopen", side_effect=error):
            with self.assertRaises(urllib.error.HTTPError):
                self.check()

    def test_explicit_matching_bootstrap_allows_first_404(self):
        error = urllib.error.HTTPError("https://api.github.com/fixture", 404, "missing", {}, None)
        with patch.object(guard, "urlopen", side_effect=error):
            self.check(bootstrap_tag="v1.2.0")

    def test_wrong_bootstrap_tag_blocks(self):
        error = urllib.error.HTTPError("https://api.github.com/fixture", 404, "missing", {}, None)
        with patch.object(guard, "urlopen", side_effect=error):
            with self.assertRaises(urllib.error.HTTPError):
                self.check(bootstrap_tag="v1.1.0")

    def test_bootstrap_never_bypasses_403(self):
        error = urllib.error.HTTPError("https://api.github.com/fixture", 403, "denied", {}, None)
        with patch.object(guard, "urlopen", side_effect=error):
            with self.assertRaises(urllib.error.HTTPError):
                self.check(bootstrap_tag="v1.2.0")

    def test_bootstrap_never_bypasses_second_page_404(self):
        first = [{"metadata": {"container": {"tags": []}}}] * 100
        error = urllib.error.HTTPError("https://api.github.com/fixture", 404, "missing", {}, None)
        with patch.object(guard, "urlopen", side_effect=[response(first), error]):
            with self.assertRaises(urllib.error.HTTPError):
                self.check(bootstrap_tag="v1.2.0")

    def test_redirect_never_forwards_token(self):
        requests = []

        class RedirectTransport(urllib.request.HTTPSHandler):
            def https_open(self, request):
                requests.append(request)
                headers = Message()
                headers["Location"] = "https://untrusted.invalid/steal"
                result = urllib.response.addinfourl(io.BytesIO(b""), headers, request.full_url, 302)
                result.msg = "Found"
                return result

        opener = urllib.request.build_opener(getattr(guard, "NoRedirect", urllib.request.HTTPRedirectHandler)(), RedirectTransport())
        with patch.object(guard, "urlopen", opener.open):
            with self.assertRaises(urllib.error.HTTPError) as raised:
                self.check()
            raised.exception.close()
        self.assertEqual(len(requests), 1)
        self.assertTrue(requests[0].full_url.startswith("https://api.github.com/users/"))

    def test_permission_denied_blocks(self):
        error = urllib.error.HTTPError("fixture", 403, "denied", {}, None)
        with patch.object(guard, "urlopen", side_effect=error):
            with self.assertRaises(urllib.error.HTTPError):
                self.check()

    def test_existing_tag_blocks(self):
        data = [{"metadata": {"container": {"tags": ["v1.2.0"]}}}]
        with patch.object(guard, "urlopen", return_value=response(data)):
            with self.assertRaises(RuntimeError):
                self.check()

    def test_unrelated_tag_allows(self):
        data = [{"metadata": {"container": {"tags": ["v1.1.0"]}}}]
        with patch.object(guard, "urlopen", return_value=response(data)):
            self.check()

    def test_second_page_blocks(self):
        first = [{"metadata": {"container": {"tags": []}}}] * 100
        second = [{"metadata": {"container": {"tags": ["v1.2.0"]}}}]
        with patch.object(guard, "urlopen", side_effect=[response(first), response(second)]) as mock:
            with self.assertRaises(RuntimeError):
                self.check()
            self.assertEqual(mock.call_count, 2)

    def test_malformed_response_blocks(self):
        with patch.object(guard, "urlopen", return_value=response({"error": "fixture"})):
            with self.assertRaises(ValueError):
                self.check()

    def test_bootstrap_never_bypasses_existing_tag(self):
        data = [{"metadata": {"container": {"tags": ["v1.2.0"]}}}]
        with patch.object(guard, "urlopen", return_value=response(data)):
            with self.assertRaises(RuntimeError):
                self.check(bootstrap_tag="v1.2.0")

    def test_other_http_errors_block_even_with_bootstrap(self):
        for status in [401, 429, 500]:
            with self.subTest(status=status):
                error = urllib.error.HTTPError("https://api.github.com/fixture", status, "blocked", {}, None)
                with patch.object(guard, "urlopen", side_effect=error):
                    with self.assertRaises(urllib.error.HTTPError):
                        self.check(bootstrap_tag="v1.2.0")

    def test_requests_stay_on_expected_endpoint(self):
        with patch.object(guard, "urlopen", return_value=response([])) as transport:
            self.check()
        request = transport.call_args.args[0]
        self.assertEqual(request.full_url, "https://api.github.com/users/fixture-owner/packages/container/fixture-package/versions?per_page=100&page=1")
        self.assertEqual(transport.call_args.kwargs["timeout"], 30)


if __name__ == "__main__":
    unittest.main()

#!/usr/bin/env python3
"""Verify that website metadata cannot broaden the updater's download authority."""
import copy
import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location("update_manifest", Path(__file__).with_name("update-manifest.py"))
manifest = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(manifest)


def release(tag="v1.6.4"):
    return {
        "tag_name": tag, "draft": False, "prerelease": False,
        "body": "Private drafts and release descriptions must not enter the feed",
        "assets": [
            {"name": name, "browser_download_url": f"https://github.com/{manifest.REPOSITORY}/releases/download/{tag}/{name}", "size": 100}
            for archive in manifest.ARCHIVES for name in (archive, archive + ".sha256")
        ],
    }


class ManifestTest(unittest.TestCase):
    def test_only_published_download_metadata_is_exposed(self):
        draft = release("v9.0.0")
        draft["draft"] = True
        result = manifest.build_manifest([release(), draft], "v1.6.4")
        self.assertEqual(result["schemaVersion"], 1)
        self.assertEqual(len(result["releases"]), 1)
        self.assertEqual(set(result["releases"][0]), {"tag_name", "draft", "prerelease", "assets"})
        self.assertEqual(len(result["releases"][0]["assets"]), 12)

    def test_partial_wrong_origin_duplicate_and_oversize_assets_cannot_be_promoted(self):
        for mutation in ("missing", "wrong-host", "duplicate", "oversize", "boolean-size", "wrong-tag"):
            with self.subTest(mutation=mutation):
                entry = release()
                if mutation == "missing": entry["assets"].pop()
                elif mutation == "wrong-host": entry["assets"][0]["browser_download_url"] = "https://example.com/payload.zip"
                elif mutation == "duplicate": entry["assets"].append(copy.deepcopy(entry["assets"][0]))
                elif mutation == "oversize": entry["assets"][0]["size"] = (512 << 20) + 1
                elif mutation == "boolean-size": entry["assets"][0]["size"] = True
                elif mutation == "wrong-tag": entry["assets"][0]["browser_download_url"] = entry["assets"][0]["browser_download_url"].replace("v1.6.4", "v1.6.3")
                with self.assertRaises(ValueError):
                    manifest.build_manifest([entry], "v1.6.4")

    def test_public_verification_requires_the_requested_complete_release(self):
        older = manifest.build_manifest([release("v1.6.3")])
        with patch.object(manifest, "read_json", return_value=older):
            with self.assertRaises(RuntimeError):
                manifest.published_manifest("v1.6.4", 0)
        current = manifest.build_manifest([release()])
        with patch.object(manifest, "read_json", return_value=current):
            self.assertEqual(manifest.published_manifest("v1.6.4", 0), current)

    def test_empty_or_duplicate_catalog_fails_without_publishing(self):
        for entries in ([], [release(), release()]):
            with self.assertRaises(ValueError):
                manifest.build_manifest(entries)

    def test_ci_credential_is_not_sent_to_website(self):
        with self.assertRaises(ValueError):
            manifest.read_json(manifest.WEBSITE, token="synthetic-ci-token")
        self.assertIsNone(manifest.NoRedirect().redirect_request(None, None, 302, "", {}, "https://example.com"))


if __name__ == "__main__":
    unittest.main()

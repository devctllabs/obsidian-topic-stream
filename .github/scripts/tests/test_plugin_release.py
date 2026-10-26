"""Reviewable plugin metadata changes and the no-op initial preparation."""

import json
import unittest

import prepare_plugin_release as p
from release_policy import Candidate, verify_tag

SHA = "a" * 40


def sources(version="0.1.0"):
    return {
        "plugin/package.json": json.dumps(
            {"name": "plugin", "version": version, "scripts": {"build": "keep"}},
            indent=4,
        )
        + "\n",
        "plugin/manifest.json": json.dumps(
            {"version": version, "minAppVersion": "1.8.0", "isDesktopOnly": False},
            indent=4,
        )
        + "\n",
        "plugin/versions.json": json.dumps({version: "1.8.0"}, indent=4) + "\n",
    }


class PluginPreparation(unittest.TestCase):
    def test_initial_synchronized_point_one_is_noop_without_formatting_churn(self):
        self.assertEqual(
            (),
            p.prepare_sources(
                sources(), Candidate("plugin", "0.1.0", "0.1.0", None, SHA), {}
            ),
        )

    def test_updates_three_files_and_preserves_historical_host_mapping(self):
        original = sources("1.2.3")
        historical = {"1.0.0": "1.6.0", "1.2.3": "1.7.0"}
        original["plugin/versions.json"] = json.dumps(historical)
        changes = p.prepare_sources(
            original, Candidate("plugin", "1.3.0", "1.3.0", "1.2.3", SHA), historical
        )
        values = {change.path: json.loads(change.contents) for change in changes}
        self.assertEqual(set(original), set(values))
        self.assertEqual("1.3.0", values["plugin/package.json"]["version"])
        self.assertEqual("1.3.0", values["plugin/manifest.json"]["version"])
        self.assertEqual("keep", values["plugin/package.json"]["scripts"]["build"])
        self.assertFalse(values["plugin/manifest.json"]["isDesktopOnly"])
        self.assertEqual(
            historical | {"1.3.0": "1.8.0"}, values["plugin/versions.json"]
        )

    def test_inconsistent_or_downgraded_source_versions_fail(self):
        for file, patch in (
            ("plugin/package.json", {"version": "0.2.0"}),
            ("plugin/manifest.json", {"version": "bad"}),
            ("plugin/manifest.json", {"minAppVersion": 42}),
        ):
            value = sources()
            value[file] = json.dumps(json.loads(value[file]) | patch)
            with (
                self.subTest(file=file, patch=patch),
                self.assertRaises((TypeError, ValueError)),
            ):
                p.prepare_sources(
                    value, Candidate("plugin", "0.1.0", "0.1.0", None, SHA), {}
                )

    def test_missing_invalid_metadata_and_modified_published_mapping_fail(self):
        for value in (
            {},
            sources() | {"plugin/versions.json": "null"},
            sources() | {"plugin/versions.json": "{bad"},
        ):
            with (
                self.subTest(sources=value),
                self.assertRaises((TypeError, ValueError)),
            ):
                p.prepare_sources(
                    value, Candidate("plugin", "0.1.0", "0.1.0", None, SHA), {}
                )
        with self.assertRaises(ValueError):
            p.prepare_sources(
                sources("1.2.3"),
                Candidate("plugin", "1.2.4", "1.2.4", "1.2.3", SHA),
                {"1.2.3": "1.7.0"},
            )

    def test_candidate_tag_binding_never_moves_an_existing_tag(self):
        result = Candidate("plugin", "0.1.0", "0.1.0", None, SHA)
        verify_tag(result, None)
        verify_tag(result, SHA)
        with self.assertRaises(ValueError):
            verify_tag(result, "b" * 40)


if __name__ == "__main__":
    unittest.main()

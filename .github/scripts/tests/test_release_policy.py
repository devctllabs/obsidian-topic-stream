"""Published component histories and explicit version intent."""

import unittest

import release_policy as p

SHA = "a" * 40


class Versions(unittest.TestCase):
    def test_product_path_map_is_component_specific(self):
        self.assertTrue(p.component_relevant(["internal/domain/item.go"], "backend"))
        self.assertFalse(p.component_relevant(["internal/domain/item.go"], "plugin"))
        self.assertTrue(p.component_relevant(["plugin/src/main.ts"], "plugin"))
        self.assertFalse(p.component_relevant(["plugin/src/main.ts"], "backend"))
        for component in ("backend", "plugin"):
            self.assertTrue(
                p.component_relevant(["api/openapi/openapi.yaml"], component)
            )
            self.assertTrue(
                p.component_relevant(["tools/oapi/server.yaml"], component)
            )

    def test_non_product_paths_do_not_create_a_release_reason(self):
        for path in ("docs/releases.md", ".github/workflows/ci.yml", "LICENSE"):
            with self.subTest(path=path):
                self.assertFalse(p.component_relevant([path], "backend"))
                self.assertFalse(p.component_relevant([path], "plugin"))

    def test_unclassified_product_path_fails_closed(self):
        with self.assertRaisesRegex(ValueError, "has no owner"):
            p.component_relevant(["new-root-product-file"], "backend")

    def test_first_stable_release_is_point_one_for_each_bump_and_component(self):
        for component, path, prefix in (
            ("backend", "internal/domain/item.go", "backend/v"),
            ("plugin", "plugin/src/main.ts", ""),
        ):
            for bump in ("major", "minor", "patch"):
                with self.subTest(component=component, bump=bump):
                    result = p.calculate_candidate(component, bump, SHA, [], [path])
                    self.assertEqual("0.1.0", result.version)
                    self.assertEqual(prefix + "0.1.0", result.tag)
                    self.assertIsNone(result.baseline)

    def test_histories_are_independent_and_choose_highest_stable_version(self):
        history = [
            p.Release("2.7.9"),
            p.Release("backend/v8.4.2"),
            p.Release("backend/v7.99.99"),
            p.Release("backend/v9.0.0", draft=True),
            p.Release("backend/v10.0.0-beta.1", prerelease=True),
            p.Release("unrelated/v200.0.0"),
        ]
        self.assertEqual(
            "8.4.3",
            p.calculate_candidate(
                "backend", "patch", SHA, history, ["internal/x.go"]
            ).version,
        )
        self.assertEqual(
            "2.8.0",
            p.calculate_candidate(
                "plugin", "minor", SHA, history, ["plugin/src/x.ts"]
            ).version,
        )

    def test_major_minor_patch_reset_lower_components(self):
        for bump, expected in (
            ("major", "3.0.0"),
            ("minor", "2.8.0"),
            ("patch", "2.7.10"),
        ):
            with self.subTest(bump=bump):
                self.assertEqual(
                    expected,
                    p.calculate_candidate(
                        "plugin", bump, SHA, [p.Release("2.7.9")], ["plugin/src/x.ts"]
                    ).version,
                )

    def test_other_component_and_docs_changes_do_not_create_a_release_reason(self):
        for component, paths in (
            ("backend", ["plugin/src/x.ts"]),
            ("plugin", ["internal/x.go"]),
            ("backend", ["docs/ci.md"]),
            ("plugin", []),
        ):
            with (
                self.subTest(component=component, paths=paths),
                self.assertRaises(ValueError),
            ):
                p.calculate_candidate(component, "patch", SHA, [], paths)

    def test_shared_contract_and_build_inputs_are_relevant_to_both(self):
        for component in ("backend", "plugin"):
            self.assertEqual(
                "0.1.0",
                p.calculate_candidate(
                    component, "patch", SHA, [], ["api/openapi/openapi.yaml"]
                ).version,
            )

    def test_malformed_supported_stable_metadata_and_invalid_requests_fail(self):
        for tag in ("backend/v1.2", "backend/v01.2.3", "backend/v1.2.3-beta"):
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                p.calculate_candidate(
                    "backend", "patch", SHA, [p.Release(tag)], ["internal/x.go"]
                )
        for component, bump, sha in (
            ("other", "patch", SHA),
            ("plugin", "auto", SHA),
            ("backend", "patch", "main"),
        ):
            with (
                self.subTest(component=component, bump=bump),
                self.assertRaises(ValueError),
            ):
                p.calculate_candidate(
                    component, bump, sha, [], ["api/openapi/openapi.yaml"]
                )


if __name__ == "__main__":
    unittest.main()

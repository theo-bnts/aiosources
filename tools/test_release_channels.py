import copy
import unittest

from release_channels import images, preview, promotion


class ReleaseChannelsTest(unittest.TestCase):
    revision = "a" * 40
    digest = "sha256:" + "b" * 64
    codeberg = "codeberg.org/example/aiosources"

    def event(self):
        tag, body = preview("example/aiosources", self.revision, self.digest, self.codeberg)
        return {
            "repository": {"full_name": "example/aiosources"},
            "release": {
                "tag_name": tag,
                "body": body,
                "draft": False,
                "prerelease": False,
            },
        }

    def test_preview_records_the_exact_image_and_never_claims_to_be_stable(self):
        tag, body = preview("Example/AIOSources", self.revision, self.digest, self.codeberg)
        self.assertEqual(tag, "build-" + self.revision[:12])
        self.assertIn("ghcr.io/example/aiosources@" + self.digest, body)
        self.assertIn("Test it before marking this release stable", body)

    def test_promotion_preserves_the_tested_digest_in_both_registries(self):
        self.assertEqual(
            promotion(self.event(), self.revision, self.codeberg),
            {
                "ghcr": "ghcr.io/example/aiosources",
                "codeberg": self.codeberg,
                "digest": self.digest,
                "tag": "build-" + self.revision[:12],
            },
        )

    def test_preview_draft_wrong_commit_registry_and_reserved_tags_cannot_promote(self):
        for key, value in [
            ("prerelease", True),
            ("draft", True),
            ("tag_name", "preview"),
            ("tag_name", "latest"),
            ("tag_name", self.revision),
            ("tag_name", "v2;echo unsafe"),
            ("tag_name", "v2\nunsafe"),
        ]:
            with self.subTest(key=key, value=value):
                event = self.event()
                event["release"][key] = value
                with self.assertRaises(ValueError):
                    promotion(event, self.revision, self.codeberg)
        for revision, codeberg in [
            ("c" * 40, self.codeberg),
            (self.revision, "codeberg.org/other/aiosources"),
        ]:
            with self.assertRaises(ValueError):
                promotion(self.event(), revision, codeberg)

    def test_missing_duplicate_and_tampered_records_fail_closed(self):
        original = self.event()
        for body in [
            None,
            "",
            original["release"]["body"] * 2,
            original["release"]["body"].replace(self.digest, "sha256:wrong"),
            original["release"]["body"].replace("ghcr.io/example", "ghcr.io/other"),
        ]:
            event = copy.deepcopy(original)
            event["release"]["body"] = body
            with self.assertRaises(ValueError):
                promotion(event, self.revision, self.codeberg)

    def test_ghcr_only_publications_and_semantic_release_tags_are_supported(self):
        tag, body = preview("example/aiosources", self.revision, self.digest)
        event = self.event()
        event["release"].update(body=body, tag_name="v2.0.1")
        result = promotion(event, self.revision)
        self.assertEqual(result["tag"], "v2.0.1")
        self.assertEqual(result["codeberg"], "")
        self.assertEqual(images("Example/AIOSources"), ("ghcr.io/example/aiosources", ""))

    def test_image_names_cannot_inject_another_registry_or_step_output(self):
        for repository in [
            "../example", "example/image\nother=value", "example/image:tag", "example/image/extra"
        ]:
            with self.assertRaises(ValueError):
                images(repository)
        for image in [
            "ghcr.io/example/image", "codeberg.org/example/image:tag",
            "codeberg.org/example/image\nkey=value"
        ]:
            with self.assertRaises(ValueError):
                images("example/aiosources", image)


if __name__ == "__main__":
    unittest.main()

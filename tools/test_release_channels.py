import copy
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from release_channels import MARKER, images, preview, promotion


class ReleaseChannelsTest(unittest.TestCase):
    revision = "a" * 40
    digest = "sha256:" + "b" * 64

    def event(self):
        tag, body = preview("example/aiosources", self.revision, self.digest)
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
        tag, body = preview("Example/AIOSources", self.revision, self.digest)
        self.assertEqual(tag, "build-" + self.revision[:12])
        self.assertIn("ghcr.io/example/aiosources@" + self.digest, body)
        self.assertIn("Test it before marking this release stable", body)
        self.assertIn("on GHCR, without rebuilding", body)
        self.assertNotIn("codeberg", body.lower())

    def test_promotion_preserves_the_tested_ghcr_digest(self):
        self.assertEqual(
            promotion(self.event(), self.revision),
            {
                "ghcr": "ghcr.io/example/aiosources",
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
            ("tag_name", "stable"),
            ("tag_name", self.revision),
            ("tag_name", "v2;echo unsafe"),
            ("tag_name", "v2\nunsafe"),
            ("tag_name", 12),
        ]:
            with self.subTest(key=key, value=value):
                event = self.event()
                event["release"][key] = value
                with self.assertRaises(ValueError):
                    promotion(event, self.revision)
        with self.assertRaises(ValueError):
            promotion(self.event(), "c" * 40)
        event = self.event()
        event["repository"]["full_name"] = "other/aiosources"
        with self.assertRaises(ValueError):
            promotion(event, self.revision)

    def test_missing_duplicate_and_tampered_records_fail_closed(self):
        original = self.event()
        for body in [
            None,
            "",
            ["invalid"],
            original["release"]["body"] * 2,
            original["release"]["body"].replace(self.digest, "sha256:wrong"),
            original["release"]["body"].replace("ghcr.io/example", "ghcr.io/other"),
            MARKER + "[] -->",
            MARKER + "true -->",
            MARKER + "invalid -->",
        ]:
            event = copy.deepcopy(original)
            event["release"]["body"] = body
            with self.assertRaises(ValueError):
                promotion(event, self.revision)
        for event in [None, [], {"release": []}]:
            with self.assertRaises(ValueError):
                promotion(event, self.revision)

    def test_ghcr_only_publications_and_semantic_release_tags_are_supported(self):
        tag, body = preview("example/aiosources", self.revision, self.digest)
        event = self.event()
        event["release"].update(body=body, tag_name="v2.0.1")
        result = promotion(event, self.revision)
        self.assertEqual(result["tag"], "v2.0.1")
        self.assertNotIn("codeberg", result)
        self.assertEqual(images("Example/AIOSources"), "ghcr.io/example/aiosources")

    def test_image_names_cannot_inject_another_registry_or_step_output(self):
        for repository in [
            "../example", "example/image\nother=value", "example/image:tag", "example/image/extra", None, []
        ]:
            with self.assertRaises(ValueError):
                images(repository)

    def test_historical_mirror_metadata_cannot_enable_a_second_registry(self):
        event = self.event()
        metadata = {"revision": self.revision, "digest": self.digest,
                    "ghcr": "ghcr.io/example/aiosources"}
        for old_mirror in ["codeberg.org/example/aiosources", "https://untrusted.invalid/image"]:
            metadata["codeberg"] = old_mirror
            event["release"]["body"] = MARKER + json.dumps(metadata) + " -->"
            self.assertEqual(promotion(event, self.revision), promotion(self.event(), self.revision))

    def test_installed_workflow_transition_only_emits_ghcr_outputs(self):
        script = Path(__file__).with_name("release_channels.py")
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            outputs = root / "outputs"
            common = [sys.executable, str(script), "--revision", self.revision,
                      "--codeberg", "codeberg.org/retired/image", "--output", str(outputs)]
            subprocess.run(common + ["images", "--repository", "example/aiosources"], check=True)
            expected = {"ghcr": "ghcr.io/example/aiosources",
                        "tags": "ghcr.io/example/aiosources:preview,ghcr.io/example/aiosources:" + self.revision}
            self.assertEqual(dict(line.split("=", 1) for line in outputs.read_text().splitlines()), expected)
            outputs.unlink()
            body = root / "body"
            subprocess.run(common + ["preview", "--repository", "example/aiosources",
                                    "--digest", self.digest, "--body", str(body)], check=True)
            self.assertNotIn("codeberg", body.read_text().lower())
            self.assertEqual(outputs.read_text(), "release_tag=build-" + self.revision[:12] + "\n")
            outputs.unlink()
            event = root / "event.json"
            event.write_text(json.dumps(self.event()))
            subprocess.run(common + ["promotion", "--event", str(event)], check=True)
            values = dict(line.split("=", 1) for line in outputs.read_text().splitlines())
            self.assertEqual(values, promotion(self.event(), self.revision))


if __name__ == "__main__":
    unittest.main()

#!/usr/bin/env python3
"""Prepare release metadata; registry and GitHub writes stay in Actions."""

import argparse
import json
import re
from pathlib import Path


MARKER = "<!-- AIOSources image "
COMPONENT = r"[a-z0-9]+(?:[._-][a-z0-9]+)*"


def images(repository, codeberg=""):
    repository = repository.lower()
    if not re.fullmatch(COMPONENT + "/" + COMPONENT, repository):
        raise ValueError("Invalid repository name")
    if codeberg and not re.fullmatch("codeberg.org/" + COMPONENT + "/" + COMPONENT, codeberg):
        raise ValueError("Invalid Codeberg image name")
    return "ghcr.io/" + repository, codeberg


def validate_revision(revision):
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise ValueError("Invalid release revision")


def preview(repository, revision, digest, codeberg=""):
    ghcr, codeberg = images(repository, codeberg)
    validate_revision(revision)
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
        raise ValueError("Invalid image digest")
    metadata = {"revision": revision, "digest": digest, "ghcr": ghcr, "codeberg": codeberg}
    body = (
        "This preview passed the automated checks. Test it before marking this release stable.\n\n"
        "Test this exact image:\n\n```yaml\nimage: " + ghcr + "@" + digest + "\n```\n\n"
        "The `preview` tag follows the newest tested build. The full commit tag identifies this build.\n\n"
        "After testing, edit this release and clear **Set as a pre-release**. Publishing it as a stable "
        "release promotes this image to `latest` and `stable` in both configured registries, without rebuilding it.\n\n"
        + MARKER + json.dumps(metadata, separators=(",", ":"), sort_keys=True) + " -->\n"
    )
    return "build-" + revision[:12], body


def promotion(event, revision, codeberg=""):
    release = event.get("release", {})
    if release.get("prerelease") is not False or release.get("draft") is not False:
        raise ValueError("Only a published stable release can update stable tags")
    tag = release.get("tag_name", "")
    if not re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}", tag):
        raise ValueError("The release tag is not a valid container tag")
    if tag in ("latest", "stable", "preview") or re.fullmatch(r"[0-9a-f]{40}", tag):
        raise ValueError("The release tag is reserved for an image channel or commit")
    repository = event.get("repository", {}).get("full_name", "")
    ghcr, codeberg = images(repository, codeberg)
    validate_revision(revision)
    matches = re.findall(re.escape(MARKER) + r"([^\n]+?) -->", release.get("body") or "")
    if len(matches) != 1:
        raise ValueError("The release must contain exactly one verified image record")
    metadata = json.loads(matches[0])
    if (
        metadata.get("revision") != revision
        or metadata.get("ghcr") != ghcr
        or metadata.get("codeberg") != codeberg
    ):
        raise ValueError("The image record does not match this repository, revision and registries")
    digest = metadata.get("digest", "")
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
        raise ValueError("Invalid image digest")
    return {"ghcr": ghcr, "codeberg": codeberg, "digest": digest, "tag": tag}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("images", "preview", "promotion"))
    parser.add_argument("--repository", default="")
    parser.add_argument("--revision", required=True)
    parser.add_argument("--digest", default="")
    parser.add_argument("--codeberg", default="")
    parser.add_argument("--event", type=Path)
    parser.add_argument("--body", type=Path)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    try:
        validate_revision(args.revision)
        if args.command == "images":
            ghcr, codeberg = images(args.repository, args.codeberg)
            tags = [
                image + ":" + tag
                for image in (ghcr, codeberg)
                if image
                for tag in ("preview", args.revision)
            ]
            result = {"tags": ",".join(tags), "ghcr": ghcr, "codeberg": codeberg}
        elif args.command == "preview":
            tag, body = preview(args.repository, args.revision, args.digest, args.codeberg)
            args.body.write_text(body)
            result = {"release_tag": tag}
        else:
            result = promotion(json.loads(args.event.read_text()), args.revision, args.codeberg)
        with args.output.open("a") as output:
            for key, value in result.items():
                output.write(key + "=" + value + "\n")
    except (ValueError, TypeError, AttributeError, OSError) as error:
        # Never print the release payload, registry credentials or an event body.
        reason = str(error) if type(error) is ValueError else "Invalid or unavailable release metadata"
        parser.exit(1, reason + ".\n")


if __name__ == "__main__":
    main()

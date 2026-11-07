import json
import os
from pathlib import Path
import tempfile
import unittest
from database import Database, private_json


class PrivateWorkspace(unittest.TestCase):
    def test_private_writes_are_atomic_and_preserve_previous_data_on_error(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "baseline.json"
            private_json(path, {"version": 1})
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            with self.assertRaises(TypeError):
                private_json(path, {"invalid": object()})
            self.assertEqual(json.loads(path.read_text()), {"version": 1})
            self.assertEqual(list(Path(directory).iterdir()), [path])

    def test_unowned_directories_are_not_modified(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            (root / "existing").write_text("retained")
            root.chmod(0o755)
            with self.assertRaisesRegex(ValueError, "unowned"):
                Database(root)
            self.assertEqual(root.stat().st_mode & 0o777, 0o755)
            self.assertEqual((root / "existing").read_text(), "retained")

    def test_owner_mismatch_is_rejected_before_database_commands(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            private_json(root / "owner.json", {"root": str(root), "uid": os.getuid() + 1})
            with self.assertRaisesRegex(ValueError, "owner mismatch"):
                Database(root)

    def test_symlink_workspace_is_not_followed(self):
        with tempfile.TemporaryDirectory() as directory:
            parent = Path(directory).resolve()
            target = parent / "target"
            target.mkdir()
            link = parent / "link"
            link.symlink_to(target, target_is_directory=True)
            with self.assertRaisesRegex(ValueError, "symbolic link"):
                Database(link)
            self.assertEqual(list(target.iterdir()), [])


if __name__ == "__main__":
    unittest.main()

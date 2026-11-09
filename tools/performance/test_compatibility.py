import tempfile
import unittest
from pathlib import Path

from compatibility import default_workspace


class ExistingWorkspaceTests(unittest.TestCase):
    def test_existing_owned_paths_are_kept_without_moving_private_data(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            previous = root / ".ingest/performance"
            previous.mkdir(parents=True)
            self.assertEqual(default_workspace(root), previous)
            current = root / ".aiosources/performance"
            current.mkdir(parents=True)
            self.assertEqual(default_workspace(root), current)

    def test_new_workspace_uses_the_application_name(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.assertEqual(default_workspace(root), root / ".aiosources/performance")


if __name__ == "__main__":
    unittest.main()

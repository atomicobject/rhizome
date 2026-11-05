import unittest
from unittest.mock import patch

import release_cli


class PublicEntrypointTest(unittest.TestCase):
    def test_local_public_mutation_and_publication_stop_before_orchestration(self):
        for command in ("cut-release", "apply", "publish", "resume"):
            with self.subTest(command=command), patch.dict("os.environ", {}, clear=True), patch.object(release_cli, "Orchestrator") as orchestrator:
                with self.assertRaisesRegex(release_cli.OrchestrationError, "reviewed release-PR CI"):
                    release_cli.main([command])
                orchestrator.assert_not_called()

    def test_internal_legacy_release_is_still_dispatched(self):
        with patch.dict("os.environ", {"RZM_INTERNAL_S3_BUCKET": "synthetic-bucket"}, clear=True), patch.object(release_cli, "Orchestrator") as orchestrator, patch.object(release_cli, "run_cli", return_value=0) as dispatch:
            self.assertEqual(release_cli.main(["cut-release"]), 0)
            dispatch.assert_called_once_with(orchestrator.return_value, ["cut-release"])


if __name__ == "__main__":
    unittest.main()

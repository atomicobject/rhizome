import unittest
from run import scoped_environment, sensitive_name
from check import forbidden_path


class SecretScopeTest(unittest.TestCase):
    def test_development_excludes_publishing_and_legacy_provider_keys(self):
        source = {key: "synthetic" for key in ("VOYAGE_API_KEY", "TYPESAFE_API_KEY", "OPENAI_API_KEY", "CEREBRAS_API_KEY", "GITHUB_TOKEN", "GH_TOKEN", "ATOMIC_RHIZOME_KEY", "OP_SERVICE_ACCOUNT_TOKEN", "BREW_GITHUB_TOKEN", "AWS_SECRET_ACCESS_KEY", "AWS_ACCESS_KEY_ID", "GOOGLE_APPLICATION_CREDENTIALS")}
        source["PATH"] = "/bin"
        self.assertEqual({"PATH": "/bin", "VOYAGE_API_KEY": "synthetic", "TYPESAFE_API_KEY": "synthetic"}, scoped_environment(source, "development"))

    def test_release_scopes_github_token_to_publish_adapter(self):
        env = scoped_environment({"GITHUB_TOKEN": "synthetic-publish", "GH_TOKEN": "synthetic-gh", "ATOMIC_RHIZOME_KEY": "synthetic-unlock", "OP_SERVICE_ACCOUNT_TOKEN": "synthetic-op", "AWS_ACCESS_KEY_ID": "synthetic-aws-id", "AWS_SECRET_ACCESS_KEY": "synthetic-aws-secret", "AWS_SESSION_TOKEN": "synthetic-aws-session", "BREW_GITHUB_TOKEN": "synthetic-brew"}, "release")
        self.assertEqual({"RZM_RELEASE_GITHUB_TOKEN": "synthetic-publish", "BREW_GITHUB_TOKEN": "synthetic-brew", "ATOMIC_RHIZOME_KEY": "synthetic-unlock", "AWS_ACCESS_KEY_ID": "synthetic-aws-id", "AWS_SECRET_ACCESS_KEY": "synthetic-aws-secret", "AWS_SESSION_TOKEN": "synthetic-aws-session"}, env)

    def test_local_env_files_cannot_be_committed(self):
        for path in (".env", "nested/.env", ".env.local", "nested/.env.production"):
            self.assertTrue(forbidden_path(path))
        self.assertFalse(forbidden_path(".env.example"))
        self.assertFalse(forbidden_path("pkg/teamkeys/keys_encrypted.go"))

    def test_common_credential_names_are_sensitive(self):
        for name in ("AWS_SECRET_ACCESS_KEY", "AWS_ACCESS_KEY_ID", "GOOGLE_APPLICATION_CREDENTIALS", "AZURE_CLIENT_SECRET", "SSH_PRIVATE_KEY"):
            self.assertTrue(sensitive_name(name), name)
        for name in ("PATH", "SSH_AUTH_SOCK", "RZM_REPO_DELEGATE"):
            self.assertFalse(sensitive_name(name), name)


if __name__ == "__main__":
    unittest.main()

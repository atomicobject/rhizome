import unittest
from unittest.mock import patch

from deploy import checked_changes, configure_alb, deploy, wait_for_revision, wait_until_settled, wait_for_stack


class DeploymentSafetyTest(unittest.TestCase):
    def test_rejects_service_replacement_and_unrelated_resources(self):
        for logical, action, replacement in [("Gateway", "Modify", "True"), ("Gateway", "Modify", "Conditional"), ("Gateway", "Remove", "False"), ("StateTable", "Modify", "False"), ("DeployRole", "Modify", "False")]:
            with self.subTest(logical=logical, action=action, replacement=replacement):
                with self.assertRaises(ValueError):
                    checked_changes([{"ResourceChange": {"LogicalResourceId": logical, "Action": action, "Replacement": replacement}}])
        checked_changes([{"ResourceChange": {"LogicalResourceId": "Gateway", "Action": "Modify", "Replacement": "False"}}, {"ResourceChange": {"LogicalResourceId": "TaskDefinition", "Action": "Modify", "Replacement": "True"}}])

    def test_timeout_readback_must_match(self):
        attrs = {"Attributes": [{"Key":"idle_timeout.timeout_seconds", "Value":"60"}]}
        with patch("deploy.aws", side_effect=[{}, attrs]) as cli:
            with self.assertRaisesRegex(ValueError, "verification failed"):
                configure_alb("synthetic-alb")
            self.assertIn("Key=idle_timeout.timeout_seconds,Value=300", cli.call_args_list[0].args)

    def test_success_requires_target_revision_and_single_task(self):
        express = {"service": {"activeConfigurations": [{"taskDefinitionArn":"new", "serviceRevisionArn":"revision-new"}]}}
        history = {"serviceDeployments": [{"targetServiceRevisionArn":"revision-new", "status":"SUCCESSFUL", "createdAt":"2026-11-01T12:00:00Z"}]}
        service = {"services": [{"taskDefinition":"new", "desiredCount":1, "runningCount":1, "pendingCount":0}]}
        with patch("deploy.aws", side_effect=[express, history, service]):
            wait_for_revision("arn:aws:ecs:region:account:service/default/test", "new", float("inf"))
        history["serviceDeployments"][0]["status"] = "ROLLBACK_SUCCESSFUL"
        with patch("deploy.aws", side_effect=[express, history]):
            with self.assertRaisesRegex(ValueError,"rolled back"):
                wait_for_revision("arn:aws:ecs:region:account:service/default/test", "new", float("inf"))

    def test_automatic_rollback_is_not_success(self):
        service={"services":[{"taskDefinition":"old", "runningCount":1, "pendingCount":0, "deployments":[{"rolloutState":"COMPLETED"}]}]}
        express={"service":{"activeConfigurations":[{"taskDefinitionArn":"old"}]}}
        with patch("deploy.aws", side_effect=[express]):
            with self.assertRaisesRegex(ValueError,"rolled back"):
                wait_for_revision("arn:aws:ecs:region:account:service/default/test", "new", float("inf"))

    def test_failed_update_still_restores_surviving_alb(self):
        outputs = {"RepositoryUri":"123456789012.dkr.ecr.us-east-2.amazonaws.com/synthetic", "LoadBalancerArn":"synthetic-alb", "Endpoint":"https://synthetic.invalid", "ServiceArn":"arn:aws:ecs:region:account:service/default/test"}
        stack = {"Stacks":[{"Parameters":[], "Outputs":[{"OutputKey":k,"OutputValue":v} for k,v in outputs.items()]}]}
        def cli(*args):
            if args[:2] == ("cloudformation", "describe-stacks"):
                return stack
            if args[:3] == ("cloudformation", "wait", "stack-update-complete"):
                raise RuntimeError("failed deployment")
            if args[:2] == ("ecs", "list-service-deployments"):
                return {"serviceDeployments":[{"status":"ROLLBACK_SUCCESSFUL"}]}
            if args[:2] == ("cloudformation", "describe-change-set"):
                return {"Changes":[{"ResourceChange":{"LogicalResourceId":"Gateway", "Action":"Modify", "Replacement":"False"}}]}
            return {}
        with patch("deploy.aws", side_effect=cli), patch("deploy.configure_alb") as alb, patch("deploy.verify_ingress"), patch("deploy.smoke") as health:
            with self.assertRaisesRegex(RuntimeError, "failed deployment"):
                deploy("synthetic-stack", "a" * 40, "synthetic-run")
            alb.assert_called_once_with("synthetic-alb")
            health.assert_called_once_with("https://synthetic.invalid")

    def test_alb_waits_for_ecs_rollback_to_finish(self):
        rolling = {"serviceDeployments":[{"status":"ROLLBACK_IN_PROGRESS"}]}
        done = {"serviceDeployments":[{"status":"ROLLBACK_SUCCESSFUL"}, {"status":"SUCCESSFUL"}]}
        with patch("deploy.aws", side_effect=[rolling, done]) as cli, patch("deploy.time.sleep"):
            wait_until_settled("arn:aws:ecs:region:account:service/default/test", float("inf"))
            self.assertEqual(2, cli.call_count)

    def test_update_rollback_is_polled_until_it_finishes(self):
        rolling = {"Stacks":[{"StackStatus":"UPDATE_ROLLBACK_IN_PROGRESS"}]}
        cleanup = {"Stacks":[{"StackStatus":"UPDATE_ROLLBACK_COMPLETE_CLEANUP_IN_PROGRESS"}]}
        done = {"Stacks":[{"StackStatus":"UPDATE_ROLLBACK_COMPLETE", "Outputs":[]}]}
        with patch("deploy.aws", side_effect=[rolling, cleanup, done]) as cli, patch("deploy.time.sleep"):
            self.assertEqual("UPDATE_ROLLBACK_COMPLETE", wait_for_stack("synthetic-stack", float("inf"))["StackStatus"])
            self.assertTrue(all(c.args[:2] == ("cloudformation", "describe-stacks") for c in cli.call_args_list))

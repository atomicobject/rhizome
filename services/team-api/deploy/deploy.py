#!/usr/bin/env python3
"""Deploy an existing bootstrap stack. AWS access belongs only to the operator/CI.

Use the previous template and parameters so account configuration never enters
Git, and reject any change that would replace the service or alter its resources.
"""
import argparse
import json
import re
import subprocess
import time
import urllib.request


def aws(*args):
    result = subprocess.run(["aws", *args, "--output", "json", "--no-cli-pager"],
                            check=True, capture_output=True, text=True)
    return json.loads(result.stdout) if result.stdout.strip() else {}


def checked_changes(changes):
    """Only a new task revision and an in-place service deployment are allowed."""
    for change in changes:
        resource = change["ResourceChange"]
        logical = resource["LogicalResourceId"]
        if logical == "TaskDefinition" and resource["Action"] == "Modify":
            continue
        if logical == "Gateway" and resource["Action"] in ("Add", "Modify"):
            if resource.get("Replacement", "False") == "False":
                continue
        raise ValueError(f"unsafe deployment change: {logical} {resource['Action']}")


def stack_outputs(stack):
    return {item["OutputKey"]: item["OutputValue"] for item in stack.get("Outputs", [])}


def wait_for_revision(service_arn, task_arn, deadline):
    # Match the canary deployment to the requested task definition. An ACTIVE
    # Express service, or a stable old task after rollback, is not success.
    cluster, service = service_arn.split(":service/", 1)[1].split("/", 1)
    while time.monotonic() < deadline:
        active = aws("ecs", "describe-express-gateway-service", "--service-arn", service_arn)["service"]
        configs = active.get("activeConfigurations", [])
        desired = next((c for c in configs if c["taskDefinitionArn"] == task_arn), None)
        if configs and desired is None:
            raise ValueError("requested task revision is no longer active; deployment may have rolled back")
        history = aws("ecs", "list-service-deployments", "--cluster", cluster, "--service", service)["serviceDeployments"]
        matches = [d for d in history if desired and d["targetServiceRevisionArn"] == desired["serviceRevisionArn"]]
        latest = max(matches, key=lambda d: d["createdAt"], default=None)
        if latest and latest["status"] in ("STOPPED", "STOP_REQUESTED", "ROLLBACK_REQUESTED", "ROLLBACK_IN_PROGRESS", "ROLLBACK_SUCCESSFUL", "ROLLBACK_FAILED"):
            raise ValueError("requested canary failed or rolled back: " + latest["status"])
        details = aws("ecs", "describe-services", "--cluster", cluster, "--services", service)
        if details.get("failures"):
            raise ValueError("ECS service lookup failed")
        item = details["services"][0]
        if (latest and latest["status"] == "SUCCESSFUL" and item["taskDefinition"] == task_arn
                and item["desiredCount"] == 1 and item["runningCount"] == 1 and item["pendingCount"] == 0):
            print("Canary successful: requested ARM64 task revision, one running task")
            return
        time.sleep(15)
    raise TimeoutError("ECS did not converge on the requested single-task revision")


UNSETTLED = ("PENDING", "IN_PROGRESS", "STOP_REQUESTED", "ROLLBACK_REQUESTED", "ROLLBACK_IN_PROGRESS")


def wait_until_settled(service_arn, deadline):
    """Wait until no ECS deployment is still running or rolling back, so ALB
    settings apply to the load balancer that survives."""
    cluster, service = service_arn.split(":service/", 1)[1].split("/", 1)
    while time.monotonic() < deadline:
        history = aws("ecs", "list-service-deployments", "--cluster", cluster, "--service", service)["serviceDeployments"]
        if not any(d["status"] in UNSETTLED for d in history):
            return
        time.sleep(15)
    raise TimeoutError("ECS deployments did not settle")


def configure_alb(arn):
    aws("elbv2", "modify-load-balancer-attributes", "--load-balancer-arn", arn,
        "--attributes", "Key=idle_timeout.timeout_seconds,Value=300",
        "Key=routing.http.xff_header_processing.mode,Value=append",
        "Key=routing.http.xff_client_port.enabled,Value=false")
    attrs = {a["Key"]: a["Value"] for a in aws("elbv2", "describe-load-balancer-attributes", "--load-balancer-arn", arn)["Attributes"]}
    expected = {"idle_timeout.timeout_seconds": "300", "routing.http.xff_header_processing.mode": "append", "routing.http.xff_client_port.enabled": "false"}
    if any(attrs.get(k) != v for k, v in expected.items()):
        raise ValueError("ALB timeout or trusted forwarding verification failed")
    print("ALB verified: idle timeout 300 s; X-Forwarded-For append; client port disabled")


def verify_ingress(outputs):
    lb = aws("elbv2", "describe-load-balancers", "--load-balancer-arns", outputs["LoadBalancerArn"])["LoadBalancers"][0]
    lb_groups = set(lb["SecurityGroups"])
    groups = [group.rsplit("/", 1)[-1] for group in outputs["ServiceSecurityGroups"].split(",") if group]
    if not groups:
        raise ValueError("no task security groups were returned")
    rules = aws("ec2", "describe-security-groups", "--group-ids", *groups)["SecurityGroups"]
    for group in rules:
        for rule in group["IpPermissions"]:
            # A direct connection to the task must never be able to spoof XFF.
            if rule.get("IpRanges") or rule.get("Ipv6Ranges") or rule.get("PrefixListIds"):
                raise ValueError("task ingress permits CIDR/prefix-list access; require ALB security groups only")
            if any(p["GroupId"] not in lb_groups for p in rule.get("UserIdGroupPairs", [])):
                raise ValueError("task ingress permits a security group other than the ALB")
    print("Task ingress verified: ALB security groups only")


def smoke(endpoint):
    if not endpoint.startswith("https://"):
        endpoint = "https://" + endpoint
    # No auth headers, redirects, custom CA overrides, or provider traffic.
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, headers, newurl):
            return None
    opener = urllib.request.build_opener(NoRedirect())
    with opener.open(endpoint.rstrip("/") + "/healthz", timeout=20) as response:
        if response.status != 200:
            raise ValueError("HTTPS health check failed")
    print("HTTPS /healthz: 200")


def deploy(stack_name, tag, deployment_id):
    if not re.fullmatch(r"[a-f0-9]{40}", tag):
        raise ValueError("image tag must be a full Git commit SHA")
    previous = aws("cloudformation", "describe-stacks", "--stack-name", stack_name)["Stacks"][0]
    outputs = stack_outputs(previous)
    registry, repo = outputs["RepositoryUri"].split("/", 1)
    aws("ecr", "describe-images", "--registry-id", registry.split(".")[0], "--repository-name", repo, "--image-ids", f"imageTag={tag}")
    params = [{"ParameterKey": p["ParameterKey"], "UsePreviousValue": True} for p in previous["Parameters"]
              if p["ParameterKey"] not in ("ImageTag", "DeploymentId", "EnableService")]
    params += [{"ParameterKey": k, "ParameterValue": v} for k, v in {"ImageTag":tag, "DeploymentId":deployment_id, "EnableService":"true"}.items()]
    change_set = "team-api-" + deployment_id
    aws("cloudformation", "create-change-set", "--stack-name", stack_name, "--change-set-name", change_set,
        "--change-set-type", "UPDATE", "--use-previous-template", "--parameters", json.dumps(params), "--capabilities", "CAPABILITY_IAM")
    aws("cloudformation", "wait", "change-set-create-complete", "--stack-name", stack_name, "--change-set-name", change_set)
    change = aws("cloudformation", "describe-change-set", "--stack-name", stack_name, "--change-set-name", change_set)
    if change.get("NextToken"):
        raise ValueError("unexpected paginated change set")
    checked_changes(change.get("Changes", []))
    # Fail before executing if a service replacement would change the hostname.
    aws("cloudformation", "execute-change-set", "--stack-name", stack_name, "--change-set-name", change_set)
    try:
        aws("cloudformation", "wait", "stack-update-complete", "--stack-name", stack_name)
        stack = aws("cloudformation", "describe-stacks", "--stack-name", stack_name)["Stacks"][0]
        outputs = stack_outputs(stack)
        if previous_endpoint := stack_outputs(previous).get("Endpoint"):
            if outputs["Endpoint"] != previous_endpoint:
                raise ValueError("gateway endpoint changed unexpectedly")
        resource = aws("cloudformation", "describe-stack-resource", "--stack-name", stack_name, "--logical-resource-id", "TaskDefinition")
        wait_for_revision(outputs["ServiceArn"], resource["StackResourceDetail"]["PhysicalResourceId"], time.monotonic() + 2400)
    finally:
        # An automatic rollback is also an Express deployment. Restore ALB
        # settings on the surviving service even when the requested update failed.
        current = aws("cloudformation", "describe-stacks", "--stack-name", stack_name)["Stacks"][0]
        if current.get("StackStatus") in ("UPDATE_ROLLBACK_IN_PROGRESS", "UPDATE_ROLLBACK_COMPLETE_CLEANUP_IN_PROGRESS"):
            aws("cloudformation", "wait", "stack-update-rollback-complete", "--stack-name", stack_name)
            current = aws("cloudformation", "describe-stacks", "--stack-name", stack_name)["Stacks"][0]
        surviving = stack_outputs(current)
        if "LoadBalancerArn" in surviving:
            wait_until_settled(surviving["ServiceArn"], time.monotonic() + 2400)
            configure_alb(surviving["LoadBalancerArn"])
            verify_ingress(surviving)
            smoke(surviving["Endpoint"])



if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--stack", required=True)
    parser.add_argument("--image-tag", required=True)
    parser.add_argument("--deployment-id", required=True)
    args = parser.parse_args()
    deploy(args.stack, args.image_tag, args.deployment_id)

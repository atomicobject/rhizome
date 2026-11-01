#!/usr/bin/env python3
"""Run the real image against isolated DynamoDB Local with synthetic inputs only."""
import json
import subprocess
import time
import urllib.request
import uuid


def docker(*args):
    return subprocess.check_output(["docker", *args], text=True).strip()


def local_request(url, operation, body):
    request = urllib.request.Request(url, json.dumps(body).encode(), headers={
        "Content-Type": "application/x-amz-json-1.0",
        "X-Amz-Target": "DynamoDB_20120810." + operation,
        "Authorization": "AWS4-HMAC-SHA256 Credential=synthetic/20261101/us-east-2/dynamodb/aws4_request, SignedHeaders=host;x-amz-date, Signature=synthetic",
        "X-Amz-Date": "20261101T000000Z",
    })
    with urllib.request.urlopen(request, timeout=5) as response:
        return json.load(response)


def main():
    name = "team-api-check-" + uuid.uuid4().hex[:12]
    containers = []
    docker("network", "create", name)
    try:
        db = docker("run", "--detach", "--rm", "--network", name, "--network-alias", "dynamodb",
                    "-p", "127.0.0.1::8000", "amazon/dynamodb-local:3.1.0", "-jar", "DynamoDBLocal.jar", "-inMemory", "-sharedDb")
        containers.append(db)
        endpoint = "http://" + docker("port", db, "8000/tcp")
        for attempt in range(60):
            try:
                local_request(endpoint, "ListTables", {})
                break
            except Exception:
                if attempt == 59:
                    raise
                time.sleep(0.5)
        local_request(endpoint, "CreateTable", {"TableName":"synthetic-state", "BillingMode":"PAY_PER_REQUEST",
                      "AttributeDefinitions":[{"AttributeName":k,"AttributeType":"S"} for k in ("pk","sk")],
                      "KeySchema":[{"AttributeName":"pk","KeyType":"HASH"},{"AttributeName":"sk","KeyType":"RANGE"}]})
        config = {"mode":"production", "listen":":8080", "data_dir":"/data", "issuer":"https://accounts.google.com",
                  "client_id":"synthetic.apps.googleusercontent.com", "accepted_domains":["example.com"],
                  "admins":["synthetic@example.com"], "diagnostics_output":"stdout",
                  "state":{"backend":"dynamodb","table":"synthetic-state","region":"us-east-2"}}
        app = docker("run", "--detach", "--rm", "--read-only", "--network", name, "-p", "127.0.0.1::8080",
                     "-e", "RHIZOME_TEAM_API_CONFIG=" + json.dumps(config), "-e", "VOYAGE_API_KEY=synthetic-voyage",
                     "-e", "TYPESAFE_API_KEY=synthetic-typesafe", "-e", "RHIZOME_TEAM_API_CLIENT_SECRET=synthetic-client",
                     "-e", "AWS_ACCESS_KEY_ID=synthetic", "-e", "AWS_SECRET_ACCESS_KEY=synthetic",
                     "-e", "AWS_EC2_METADATA_DISABLED=true", "-e", "AWS_ENDPOINT_URL_DYNAMODB=http://dynamodb:8000",
                     "rhizome-team-api:deployment-check")
        containers.append(app)
        origin = "http://" + docker("port", app, "8080/tcp")
        for attempt in range(60):
            try:
                with urllib.request.urlopen(origin + "/healthz", timeout=2) as response:
                    assert response.status == 200
                break
            except Exception:
                if attempt == 59:
                    print(docker("logs", app))
                    raise
                time.sleep(0.5)
        print("Container /healthz: 200 (production config from RHIZOME_TEAM_API_CONFIG)")
        for pk in ("owners", "config"):
            result = local_request(endpoint, "Query", {"TableName":"synthetic-state", "ConsistentRead":True,
                     "KeyConditionExpression":"pk = :pk", "ExpressionAttributeValues":{":pk":{"S":pk}}})
            assert result["Count"] > 0, "gateway did not initialize " + pk
        print("DynamoDB Local: owner lease and runtime configuration persisted")
        assert docker("image", "inspect", "--format", "{{.Architecture}}", "rhizome-team-api:deployment-check") == "arm64"
        info = json.loads(docker("inspect", app))[0]
        assert info["Config"]["User"] == "65532:65532"
        assert info["HostConfig"]["ReadonlyRootfs"]
        events = [json.loads(line) for line in docker("logs", app).splitlines()]
        assert any(event.get("event") == "teamapi.request" for event in events)
        print("Container: ARM64, UID 65532, read-only filesystem, JSON-line request diagnostics")
    finally:
        for container in reversed(containers):
            subprocess.run(["docker", "stop", container], check=False, stdout=subprocess.DEVNULL)
        docker("network", "rm", name)


if __name__ == "__main__":
    main()

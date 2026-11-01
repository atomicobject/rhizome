// Package dynamotest provisions disposable tables only on DynamoDB Local.
// It never loads the AWS default credential chain.
package dynamotest

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/atomicobject/rhizome/services/team-api/internal/dynamotable"
)

func Open(t *testing.T) *dynamotable.Table {
	t.Helper()
	endpoint := os.Getenv("TEAMAPI_DYNAMODB_ENDPOINT")
	if endpoint == "" {
		t.Skip("set TEAMAPI_DYNAMODB_ENDPOINT to run against DynamoDB Local")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		t.Fatal("TEAMAPI_DYNAMODB_ENDPOINT must be a loopback HTTP DynamoDB Local endpoint")
	}
	client := dynamodb.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("synthetic", "synthetic", ""),
		HTTPClient:  &http.Client{Timeout: 10 * time.Second},
	}, func(o *dynamodb.Options) { o.BaseEndpoint = aws.String(endpoint) })
	name := "teamapi-test-" + dynamotable.ID()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(name), BillingMode: types.BillingModePayPerRequest,
		KeySchema:            []types.KeySchemaElement{{AttributeName: aws.String("pk"), KeyType: types.KeyTypeHash}, {AttributeName: aws.String("sk"), KeyType: types.KeyTypeRange}},
		AttributeDefinitions: []types.AttributeDefinition{{AttributeName: aws.String("pk"), AttributeType: types.ScalarAttributeTypeS}, {AttributeName: aws.String("sk"), AttributeType: types.ScalarAttributeTypeS}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := client.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: aws.String(name)}); err != nil {
			t.Error(err)
		}
	})
	if err := dynamodb.NewTableExistsWaiter(client).Wait(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(name)}, 20*time.Second); err != nil {
		t.Fatal(err)
	}
	return &dynamotable.Table{Client: client, Name: name}
}

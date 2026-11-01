// Package dynamotable is the DynamoDB representation shared by the identity
// store and ledger. Business operations own their transactions and retries.
package dynamotable

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Table uses string partition/sort keys pk/sk. Each item holds a JSON body
// and an opaque revision. Reads are strongly consistent; all writes compare
// revisions, including condition-only reads used by a business transaction.
type Table struct {
	Client *dynamodb.Client
	Name   string
}

type Item struct {
	PK, SK, Revision string
	Body             []byte
}

func key(pk, sk string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"pk": &types.AttributeValueMemberS{Value: pk}, "sk": &types.AttributeValueMemberS{Value: sk}}
}
func token() string {
	var b [16]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func decode(v map[string]types.AttributeValue) Item {
	str := func(k string) string {
		if s, ok := v[k].(*types.AttributeValueMemberS); ok {
			return s.Value
		}
		return ""
	}
	return Item{PK: str("pk"), SK: str("sk"), Revision: str("rev"), Body: []byte(str("body"))}
}
func (i Item) Decode(dst any) error {
	if i.Revision == "" {
		return nil
	}
	return json.Unmarshal(i.Body, dst)
}
func (t *Table) Get(ctx context.Context, pk, sk string, dst any) (Item, error) {
	out, err := t.Client.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String(t.Name), Key: key(pk, sk), ConsistentRead: aws.Bool(true)})
	if err != nil {
		return Item{}, err
	}
	i := decode(out.Item)
	i.PK, i.SK = pk, sk
	if dst != nil {
		err = i.Decode(dst)
	}
	return i, err
}

// Put and Check use the revision from Get. An absent revision requires an
// absent item. Put always assigns a fresh revision, including no-op updates.
func (t *Table) Put(i Item, body any) (types.TransactWriteItem, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return types.TransactWriteItem{}, err
	}
	attrs := key(i.PK, i.SK)
	attrs["rev"] = &types.AttributeValueMemberS{Value: token()}
	attrs["body"] = &types.AttributeValueMemberS{Value: string(b)}
	cond, vals := condition(i)
	return types.TransactWriteItem{Put: &types.Put{TableName: aws.String(t.Name), Item: attrs, ConditionExpression: aws.String(cond), ExpressionAttributeValues: vals}}, nil
}
func (t *Table) Check(i Item) types.TransactWriteItem {
	cond, vals := condition(i)
	return types.TransactWriteItem{ConditionCheck: &types.ConditionCheck{TableName: aws.String(t.Name), Key: key(i.PK, i.SK), ConditionExpression: aws.String(cond), ExpressionAttributeValues: vals}}
}
func condition(i Item) (string, map[string]types.AttributeValue) {
	if i.Revision == "" {
		return "attribute_not_exists(pk)", nil
	}
	return "rev = :rev", map[string]types.AttributeValue{":rev": &types.AttributeValueMemberS{Value: i.Revision}}
}

// Write retries only definite conditional conflicts via Retry. Ambiguous
// transport errors escape unchanged. The token makes SDK transport retries
// of this exact transaction idempotent; durable receipts outlive that token.
func (t *Table) Write(ctx context.Context, writes ...types.TransactWriteItem) error {
	_, err := t.Client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: writes, ClientRequestToken: aws.String(token())})
	var canceled *types.TransactionCanceledException
	if errors.As(err, &canceled) {
		conflict := false
		for _, r := range canceled.CancellationReasons {
			switch aws.ToString(r.Code) {
			case "ConditionalCheckFailed", "TransactionConflict":
				conflict = true
			case "None", "":
			default:
				return err
			}
		}
		if conflict {
			return errConflict
		}
	}
	var conflict *types.TransactionConflictException
	if errors.As(err, &conflict) {
		return errConflict
	}
	return err
}

var errConflict = errors.New("dynamodb: concurrent write")

// Retry reruns a business operation after a definite CAS conflict, with
// bounded jitter and a deadline. It never retries an uncertain commit.
func Retry(ctx context.Context, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	for {
		err := fn(ctx)
		if !errors.Is(err, errConflict) {
			return err
		}
		timer := time.NewTimer(time.Duration(1+rand.IntN(30)) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("dynamodb: retry contention: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

// Query walks every page of one type partition. reverse and limit are used
// by the audit log; limit <= 0 reads the whole partition.
func (t *Table) Query(ctx context.Context, pk string, reverse bool, limit int) ([]Item, error) {
	in := &dynamodb.QueryInput{TableName: aws.String(t.Name), ConsistentRead: aws.Bool(true), KeyConditionExpression: aws.String("pk = :pk"), ExpressionAttributeValues: map[string]types.AttributeValue{":pk": &types.AttributeValueMemberS{Value: pk}}, ScanIndexForward: aws.Bool(!reverse)}
	var result []Item
	for {
		if limit > 0 {
			in.Limit = aws.Int32(int32(limit - len(result)))
		}
		out, err := t.Client.Query(ctx, in)
		if err != nil {
			return nil, err
		}
		for _, attrs := range out.Items {
			result = append(result, decode(attrs))
		}
		if len(out.LastEvaluatedKey) == 0 || (limit > 0 && len(result) >= limit) {
			return result, nil
		}
		in.ExclusiveStartKey = out.LastEvaluatedKey
	}
}

// NewItem is a known-absent key for an insert-only transaction.
func NewItem(pk, sk string) Item { return Item{PK: pk, SK: sk} }
func ID() string                 { return token() }

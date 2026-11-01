package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/dynamotest"
	"github.com/atomicobject/rhizome/services/team-api/internal/store"
	"github.com/atomicobject/rhizome/services/team-api/internal/storetest"
)

func TestDynamoDBContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store { return store.NewDynamoDB(dynamotest.Open(t)) })
}

func TestDynamoDBFailedAuditRollsBackMutation(t *testing.T) {
	st := store.NewDynamoDB(dynamotest.Open(t))
	ctx := context.Background()
	_, err := st.UpsertPerson(ctx, "person", "a@example.test", "example.test", time.Now())
	require.NoError(t, err)
	disabled := wire.PersonDisabled
	_, err = st.UpdatePerson(ctx, "person", wire.AdminPersonUpdate{Status: &disabled}, wire.AuditEntry{Detail: strings.Repeat("x", 410<<10)})
	require.Error(t, err, "DynamoDB rejects an audit item over 400 KiB")
	p, err := st.Person(ctx, "person")
	require.NoError(t, err)
	require.Equal(t, wire.PersonActive, p.Status)
	audit, err := st.Audit(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, audit)
}

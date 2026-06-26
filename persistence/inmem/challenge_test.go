package inmem_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm-server/challenge"
	"github.com/flarexio/mdm-server/persistence/inmem"
)

func TestChallengeStore_GenerateAndRedeem(t *testing.T) {
	store, err := inmem.NewChallengeStore(5 * time.Minute)
	require.NoError(t, err)

	pw, err := store.Generate("device-0001")
	require.NoError(t, err)
	assert.NotEmpty(t, pw)

	subject, err := store.Redeem(pw)
	require.NoError(t, err)
	assert.Equal(t, "device-0001", subject, "redeem returns the bound subject")
}

func TestChallengeStore_SingleUse(t *testing.T) {
	store, err := inmem.NewChallengeStore(5 * time.Minute)
	require.NoError(t, err)

	pw, err := store.Generate("device-0001")
	require.NoError(t, err)

	_, err = store.Redeem(pw)
	require.NoError(t, err)

	// Second redemption must fail: a challenge is good exactly once.
	_, err = store.Redeem(pw)
	assert.ErrorIs(t, err, challenge.ErrChallengeNotFound)
}

func TestChallengeStore_Expired(t *testing.T) {
	// A non-positive TTL means every challenge is born already expired, which lets
	// us test expiry without sleeping.
	store, err := inmem.NewChallengeStore(-1 * time.Second)
	require.NoError(t, err)

	pw, err := store.Generate("device-0001")
	require.NoError(t, err)

	_, err = store.Redeem(pw)
	assert.ErrorIs(t, err, challenge.ErrChallengeExpired)

	// Even expired, it was consumed and cannot be retried.
	_, err = store.Redeem(pw)
	assert.ErrorIs(t, err, challenge.ErrChallengeNotFound)
}

func TestChallengeStore_UnknownChallenge(t *testing.T) {
	store, err := inmem.NewChallengeStore(5 * time.Minute)
	require.NoError(t, err)

	_, err = store.Redeem("never-issued")
	assert.ErrorIs(t, err, challenge.ErrChallengeNotFound)
}

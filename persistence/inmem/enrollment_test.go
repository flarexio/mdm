package inmem_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm/enrollment"
	"github.com/flarexio/mdm/persistence/inmem"
)

func TestEnrollmentRepository(t *testing.T) {
	repo, err := inmem.NewEnrollmentRepository()
	require.NoError(t, err)

	e := enrollment.NewEnrollment("device-0001", "UDID-0001")
	require.NoError(t, repo.Store(e))

	t.Run("find by id", func(t *testing.T) {
		got, err := repo.Find("device-0001")
		require.NoError(t, err)
		assert.Equal(t, enrollment.ID("device-0001"), got.ID)
		assert.Equal(t, enrollment.Pending, got.Status)
	})

	t.Run("find by udid", func(t *testing.T) {
		got, err := repo.FindByUDID("UDID-0001")
		require.NoError(t, err)
		assert.Equal(t, enrollment.ID("device-0001"), got.ID)
	})

	t.Run("unknown id", func(t *testing.T) {
		_, err := repo.Find("nope")
		assert.ErrorIs(t, err, enrollment.ErrEnrollmentNotFound)
	})
}

func TestEnrollmentRepository_StorePersistsTransitions(t *testing.T) {
	repo, err := inmem.NewEnrollmentRepository()
	require.NoError(t, err)

	e := enrollment.NewEnrollment("device-0001", "UDID-0001")
	require.NoError(t, repo.Store(e))

	e.UpdateToken(enrollment.Push{PushMagic: "MAGIC", Token: []byte{0x01}})
	require.NoError(t, repo.Store(e))

	got, err := repo.Find("device-0001")
	require.NoError(t, err)
	assert.Equal(t, enrollment.Enrolled, got.Status)
	assert.True(t, got.CanPush())
}

func TestEnrollmentRepository_Delete(t *testing.T) {
	repo, err := inmem.NewEnrollmentRepository()
	require.NoError(t, err)

	e := enrollment.NewEnrollment("device-0001", "UDID-0001")
	require.NoError(t, repo.Store(e))
	require.NoError(t, repo.Delete(e))

	_, err = repo.Find("device-0001")
	assert.ErrorIs(t, err, enrollment.ErrEnrollmentNotFound)

	_, err = repo.FindByUDID("UDID-0001")
	assert.ErrorIs(t, err, enrollment.ErrEnrollmentNotFound)
}

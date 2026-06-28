package badger_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm/enrollment"
	badgerdb "github.com/flarexio/mdm/persistence/badger"
)

func sample(id enrollment.ID, udid string) *enrollment.Enrollment {
	e := enrollment.NewEnrollment(id, udid)
	e.UpdateToken(enrollment.Push{Topic: "com.apple.mgmt.External.x", PushMagic: "magic", Token: []byte{0x01, 0x02}})
	return e
}

func TestEnrollmentRepository(t *testing.T) {
	repo, err := badgerdb.NewEnrollmentRepository(t.TempDir())
	require.NoError(t, err)
	defer repo.Close()

	require.NoError(t, repo.Store(sample("device-0001", "UDID-1")))

	t.Run("find by id", func(t *testing.T) {
		e, err := repo.Find("device-0001")
		require.NoError(t, err)
		assert.Equal(t, "UDID-1", e.UDID)
		assert.Equal(t, enrollment.Enrolled, e.Status)
		assert.True(t, e.CanPush())
	})

	t.Run("find by udid", func(t *testing.T) {
		e, err := repo.FindByUDID("UDID-1")
		require.NoError(t, err)
		assert.Equal(t, enrollment.ID("device-0001"), e.ID)
	})

	t.Run("list all", func(t *testing.T) {
		require.NoError(t, repo.Store(sample("device-0002", "UDID-2")))
		all, err := repo.ListAll()
		require.NoError(t, err)
		assert.Len(t, all, 2)
	})

	t.Run("unknown id is ErrEnrollmentNotFound", func(t *testing.T) {
		_, err := repo.Find("nope")
		assert.ErrorIs(t, err, enrollment.ErrEnrollmentNotFound)
	})

	t.Run("delete", func(t *testing.T) {
		require.NoError(t, repo.Delete(sample("device-0002", "UDID-2")))
		_, err := repo.Find("device-0002")
		assert.ErrorIs(t, err, enrollment.ErrEnrollmentNotFound)
		_, err = repo.FindByUDID("UDID-2")
		assert.ErrorIs(t, err, enrollment.ErrEnrollmentNotFound)
	})
}

// TestPersistsAcrossReopen is the point of BadgerDB: data survives a restart.
func TestPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()

	repo, err := badgerdb.NewEnrollmentRepository(dir)
	require.NoError(t, err)
	require.NoError(t, repo.Store(sample("device-0001", "UDID-1")))
	require.NoError(t, repo.Close())

	reopened, err := badgerdb.NewEnrollmentRepository(dir)
	require.NoError(t, err)
	defer reopened.Close()

	e, err := reopened.Find("device-0001")
	require.NoError(t, err)
	assert.Equal(t, "UDID-1", e.UDID)
	assert.True(t, e.CanPush(), "push credentials must survive the restart")
}

package enrollment_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm/enrollment"
)

func TestEnrollmentLifecycle(t *testing.T) {
	e := enrollment.NewEnrollment("device-0001", "UDID-0001")

	t.Run("authenticate starts pending and unreachable", func(t *testing.T) {
		assert.Equal(t, enrollment.Pending, e.Status)
		assert.False(t, e.CanPush(), "no push credentials yet")
		require.Len(t, e.Events(), 1)
		assert.Equal(t, "enrollment_authenticated", e.Events()[0].EventName())
	})

	t.Run("token update makes it enrolled and reachable", func(t *testing.T) {
		e.UpdateToken(enrollment.Push{
			Topic:     "com.apple.mgmt.External.abc",
			PushMagic: "MAGIC-123",
			Token:     []byte{0x01, 0x02, 0x03, 0x04},
		})

		assert.Equal(t, enrollment.Enrolled, e.Status)
		assert.True(t, e.CanPush(), "now has token + magic")
	})

	t.Run("check out removes it", func(t *testing.T) {
		e.CheckOut()

		assert.Equal(t, enrollment.Removed, e.Status)
		assert.False(t, e.DeletedAt.IsZero())
		assert.False(t, e.CanPush())
	})
}

func TestCanPush_RequiresTokenAndMagic(t *testing.T) {
	e := enrollment.NewEnrollment("device-0001", "UDID-0001")

	// Enrolled status alone is not enough — the push fields must be present.
	e.UpdateToken(enrollment.Push{PushMagic: "MAGIC", Token: nil})
	assert.False(t, e.CanPush(), "missing token")

	e.UpdateToken(enrollment.Push{PushMagic: "", Token: []byte{0x01}})
	assert.False(t, e.CanPush(), "missing push magic")

	e.UpdateToken(enrollment.Push{PushMagic: "MAGIC", Token: []byte{0x01}})
	assert.True(t, e.CanPush())
}

func TestStatusParse(t *testing.T) {
	for _, s := range []enrollment.Status{enrollment.Pending, enrollment.Enrolled, enrollment.Removed} {
		parsed, err := enrollment.ParseStatus(s.String())
		require.NoError(t, err)
		assert.Equal(t, s, parsed)
	}

	_, err := enrollment.ParseStatus("bogus")
	assert.Error(t, err)
}

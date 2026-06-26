package checkin_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm-server/checkin"
)

const authenticateMsg = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>MessageType</key>
	<string>Authenticate</string>
	<key>UDID</key>
	<string>UDID-0001</string>
	<key>Topic</key>
	<string>com.apple.mgmt.External.abc</string>
	<key>SerialNumber</key>
	<string>C02ABCDEF</string>
</dict>
</plist>`

// Token <data> is base64 "AQIDBA==" == bytes 0x01 0x02 0x03 0x04.
const tokenUpdateMsg = `<plist version="1.0">
<dict>
	<key>MessageType</key>
	<string>TokenUpdate</string>
	<key>UDID</key>
	<string>UDID-0001</string>
	<key>Topic</key>
	<string>com.apple.mgmt.External.abc</string>
	<key>PushMagic</key>
	<string>MAGIC-123</string>
	<key>Token</key>
	<data>AQIDBA==</data>
</dict>
</plist>`

const checkOutMsg = `<plist version="1.0">
<dict>
	<key>MessageType</key>
	<string>CheckOut</string>
	<key>UDID</key>
	<string>UDID-0001</string>
</dict>
</plist>`

func TestDecodeCheckin(t *testing.T) {
	t.Run("authenticate", func(t *testing.T) {
		msg, err := checkin.DecodeCheckin([]byte(authenticateMsg))
		require.NoError(t, err)

		auth, ok := msg.(*checkin.Authenticate)
		require.True(t, ok, "expected *Authenticate, got %T", msg)

		assert.Equal(t, "UDID-0001", auth.UDID)
		assert.Equal(t, "C02ABCDEF", auth.SerialNumber)
		assert.NotEmpty(t, auth.Raw)
	})

	t.Run("token update carries push credentials", func(t *testing.T) {
		msg, err := checkin.DecodeCheckin([]byte(tokenUpdateMsg))
		require.NoError(t, err)

		tu, ok := msg.(*checkin.TokenUpdate)
		require.True(t, ok, "expected *TokenUpdate, got %T", msg)

		assert.Equal(t, "UDID-0001", tu.UDID)
		assert.Equal(t, "MAGIC-123", tu.PushMagic)
		assert.Equal(t, []byte{0x01, 0x02, 0x03, 0x04}, tu.Token)
		assert.Equal(t, "01020304", tu.TokenHex())
	})

	t.Run("check out", func(t *testing.T) {
		msg, err := checkin.DecodeCheckin([]byte(checkOutMsg))
		require.NoError(t, err)

		_, ok := msg.(*checkin.CheckOut)
		assert.True(t, ok, "expected *CheckOut, got %T", msg)
	})
}

func TestDecodeCheckin_Errors(t *testing.T) {
	_, err := checkin.DecodeCheckin(nil)
	assert.ErrorIs(t, err, checkin.ErrEmptyMessage)

	// A real Apple message type we deliberately do not model yet: the discriminator
	// must reject it rather than silently producing a zero value.
	unknown := `<plist version="1.0"><dict>
		<key>MessageType</key><string>GetBootstrapToken</string>
	</dict></plist>`
	_, err = checkin.DecodeCheckin([]byte(unknown))
	assert.ErrorIs(t, err, checkin.ErrUnrecognizedMessageType)
}

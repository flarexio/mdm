package command_test

import (
	"fmt"

	"github.com/flarexio/mdm/command"
)

// ExampleDeviceInformation shows a command's full round trip: build the request,
// then decode the device's result. The response decoder is selected by the
// command's RequestType, since a device result carries no type of its own.
func ExampleDeviceInformation() {
	// Request side: build a deliverable command from the typed Request.
	cmd, err := command.Build(command.DeviceInformation{
		Queries: []string{"DeviceName", "OSVersion"},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("request:", cmd.Command.RequestType)

	// Response side: the device later replies with this result plist. Decode it
	// into the typed domain model, keyed by the command's RequestType.
	result := []byte(`<plist version="1.0"><dict>
		<key>Status</key><string>Acknowledged</string>
		<key>QueryResponses</key><dict>
			<key>DeviceName</key><string>My iPhone</string>
			<key>OSVersion</key><string>17.0</string>
		</dict>
	</dict></plist>`)

	resp, err := command.DecodeResponse(cmd.Command.RequestType, result)
	if err != nil {
		panic(err)
	}

	info := resp.(command.DeviceInformationResponse)
	fmt.Println("response DeviceName:", info.QueryResponses["DeviceName"])

	// Output:
	// request: DeviceInformation
	// response DeviceName: My iPhone
}

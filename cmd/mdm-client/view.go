package main

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m model) render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", headerStyle.Render("MDM Client"))

	switch m.state {
	case stateToken:
		fmt.Fprintf(&b, "Admin token:\n%s\n\n%s",
			m.input.View(), hintStyle.Render("enter to continue · ctrl+c quit"))

	case stateDevices:
		switch {
		case m.err != nil:
			fmt.Fprintf(&b, "%s\n\n%s",
				errorStyle.Render("error: "+m.err.Error()),
				hintStyle.Render("r retry · q quit"))
		case m.devices == nil:
			b.WriteString("Loading devices…")
		case len(m.devices) == 0:
			fmt.Fprintf(&b, "No enrolled devices.\n\n%s", hintStyle.Render("r retry · q quit"))
		default:
			b.WriteString("Choose a device:\n")
			for i, d := range m.devices {
				label := fmt.Sprintf("%s (%s, %s)", d.ID, d.Status, d.UDID)
				fmt.Fprintf(&b, "%s\n", option(i == m.deviceCursor, label))
			}
			fmt.Fprintf(&b, "\n%s", hintStyle.Render("↑/↓ move · enter select · q quit"))
		}

	case stateCommand:
		fmt.Fprintf(&b, "Device: %s\n\nChoose a command:\n", selStyle.Render(m.subject))
		for i, c := range commandsList {
			fmt.Fprintf(&b, "%s\n", option(i == m.cmdCursor, c))
		}
		fmt.Fprintf(&b, "\n%s", hintStyle.Render("↑/↓ move · enter select · q quit"))

	case stateQueries:
		b.WriteString("DeviceInformation — pick queries:\n")
		for i, q := range deviceInfoQueries {
			box := "[ ]"
			if m.selected[i] {
				box = okStyle.Render("[x]")
			}
			fmt.Fprintf(&b, "%s\n", option(i == m.queryCursor, box+" "+q))
		}
		fmt.Fprintf(&b, "\n%s", hintStyle.Render("↑/↓ move · space toggle · enter send · esc back"))

	case stateWaiting:
		fmt.Fprintf(&b, "%s waiting for device response…\n", m.spinner.View())
		if m.uuid != "" {
			b.WriteString(hintStyle.Render("command " + m.uuid))
		}

	case stateResult:
		fmt.Fprintf(&b, "%s\n\n%s",
			m.resultView(), hintStyle.Render("any key: another command · q quit"))
	}

	return b.String()
}

func option(selected bool, label string) string {
	if selected {
		return cursorStyle.Render("❯ " + label)
	}
	return "  " + label
}

func (m model) resultView() string {
	switch {
	case m.err != nil:
		return errorStyle.Render("error: " + m.err.Error())
	case m.result == nil:
		return errorStyle.Render(m.status) // timeout
	}

	evt := m.result
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", okStyle.Render(fmt.Sprintf("%s — %s", evt.RequestType, evt.Status)))
	fmt.Fprintf(&b, "%s\n", hintStyle.Render("command "+evt.CommandUUID))

	if evt.Response != nil {
		pretty, _ := json.MarshalIndent(evt.Response, "", "  ")
		fmt.Fprintf(&b, "\n%s", pretty)
	}
	if len(evt.ErrorChain) > 0 {
		pretty, _ := json.MarshalIndent(evt.ErrorChain, "", "  ")
		fmt.Fprintf(&b, "\n%s\n%s", errorStyle.Render("error chain:"), pretty)
	}
	return b.String()
}

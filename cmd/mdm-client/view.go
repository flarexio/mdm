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
	b.WriteString(headerStyle.Render("MDM Client"))
	b.WriteString("\n\n")

	switch m.state {
	case stateToken:
		b.WriteString("Admin token:\n")
		b.WriteString(m.input.View())
		b.WriteString("\n\n" + hintStyle.Render("enter to continue · ctrl+c quit"))

	case stateSubject:
		b.WriteString("Target device (enrollment id):\n")
		b.WriteString(m.input.View())
		b.WriteString("\n\n" + hintStyle.Render("enter to continue · ctrl+c quit"))

	case stateCommand:
		fmt.Fprintf(&b, "Device: %s\n\n", selStyle.Render(m.subject))
		b.WriteString("Choose a command:\n")
		for i, c := range commandsList {
			b.WriteString(option(i == m.cmdCursor, c) + "\n")
		}
		b.WriteString("\n" + hintStyle.Render("↑/↓ move · enter select · q quit"))

	case stateQueries:
		b.WriteString("DeviceInformation — pick queries:\n")
		for i, q := range deviceInfoQueries {
			box := "[ ]"
			if m.selected[i] {
				box = okStyle.Render("[x]")
			}
			b.WriteString(option(i == m.queryCursor, box+" "+q) + "\n")
		}
		b.WriteString("\n" + hintStyle.Render("↑/↓ move · space toggle · enter send · esc back"))

	case stateWaiting:
		b.WriteString(m.spinner.View() + " waiting for device response…\n")
		if m.uuid != "" {
			b.WriteString(hintStyle.Render("command " + m.uuid))
		}

	case stateResult:
		b.WriteString(m.resultView())
		b.WriteString("\n\n" + hintStyle.Render("any key: another command · q quit"))
	}

	return b.String()
}

func option(selected bool, label string) string {
	if selected {
		return cursorStyle.Render("❯ "+label)
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
	b.WriteString(hintStyle.Render("command "+evt.CommandUUID) + "\n")

	if evt.Response != nil {
		pretty, _ := json.MarshalIndent(evt.Response, "", "  ")
		b.WriteString("\n" + string(pretty))
	}
	if len(evt.ErrorChain) > 0 {
		pretty, _ := json.MarshalIndent(evt.ErrorChain, "", "  ")
		b.WriteString("\n" + errorStyle.Render("error chain:") + "\n" + string(pretty))
	}
	return b.String()
}

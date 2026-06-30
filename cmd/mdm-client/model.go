package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/nats-io/nats.go"

	"github.com/flarexio/mdm/command"
)

type state int

const (
	stateToken state = iota
	stateDevices
	stateCommand
	stateQueries
	stateWaiting
	stateResult
)

// device is one row from GET /enrollments.
type device struct {
	ID      string `json:"id"`
	UDID    string `json:"udid"`
	Status  string `json:"status"`
	CanPush bool   `json:"can_push"`
}

// commandsList are the commands the client offers; they must be registered on
// the server (see command.Register).
var commandsList = []string{"DeviceInformation", "DeviceLock"}

// deviceInfoQueries are the DeviceInformation properties offered for multi-select.
var deviceInfoQueries = []string{
	"DeviceName", "OSVersion", "BuildVersion", "ProductName", "ModelName",
	"Model", "SerialNumber", "DeviceCapacity", "AvailableDeviceCapacity",
	"BatteryLevel", "IsSupervised", "WiFiMAC", "BluetoothMAC",
}

type model struct {
	url     string
	nc      *nats.Conn
	timeout time.Duration
	http    *http.Client

	state   state
	input   textinput.Model
	spinner spinner.Model

	token   string
	subject string

	devices      []device
	deviceCursor int

	cmdCursor int

	queryCursor int
	selected    map[int]bool

	uuid   string
	result *command.RespondedEvent
	status string // set on timeout
	err    error
}

func newModel(url string, nc *nats.Conn, timeout time.Duration) model {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = "paste your admin token"
	ti.EchoMode = textinput.EchoPassword // shows ***
	ti.Focus()                           // focus now: Init() can't return a mutated model

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	return model{
		url:      url,
		nc:       nc,
		timeout:  timeout,
		http:     &http.Client{Timeout: 15 * time.Second},
		input:    ti,
		spinner:  sp,
		selected: map[int]bool{},
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

type devicesMsg struct{ devices []device }
type enqueuedMsg struct{ uuid string }
type respondedMsg struct{ evt *command.RespondedEvent }
type timeoutMsg struct{}
type errMsg struct{ err error }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.handleKey(msg)

	case tea.PasteMsg:
		// Bracketed paste (e.g. pasting the token) arrives as its own message, not
		// as key presses; forward it to the focused input.
		if m.state == stateToken {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		return m, nil

	case spinner.TickMsg:
		if m.state != stateWaiting {
			return m, nil // stop ticking once the wait is over
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case devicesMsg:
		m.devices = msg.devices
		m.deviceCursor = 0
		return m, nil

	case enqueuedMsg:
		m.uuid = msg.uuid
		return m, tea.Batch(m.wait(msg.uuid), m.spinner.Tick)

	case respondedMsg:
		m.result = msg.evt
		m.state = stateResult
		return m, nil

	case timeoutMsg:
		m.status = fmt.Sprintf("timed out after %s waiting for a response", m.timeout)
		m.state = stateResult
		return m, nil

	case errMsg:
		m.err = msg.err
		if m.state == stateWaiting {
			m.state = stateResult // device-fetch errors stay on the picker for retry
		}
		return m, nil
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case stateToken:
		if msg.String() == "enter" {
			if strings.TrimSpace(m.input.Value()) == "" {
				return m, nil
			}
			m.token = m.input.Value()
			m.state = stateDevices
			return m, m.fetchDevices()
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case stateDevices:
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "r":
			if m.err != nil {
				m.err = nil
				m.devices = nil
				return m, m.fetchDevices()
			}
		case "up", "k":
			if m.deviceCursor > 0 {
				m.deviceCursor--
			}
		case "down", "j":
			if m.deviceCursor < len(m.devices)-1 {
				m.deviceCursor++
			}
		case "enter":
			if len(m.devices) > 0 {
				m.subject = m.devices[m.deviceCursor].ID
				m.state = stateCommand
			}
		}
		return m, nil

	case stateCommand:
		switch msg.String() {
		case "up", "k":
			if m.cmdCursor > 0 {
				m.cmdCursor--
			}
		case "down", "j":
			if m.cmdCursor < len(commandsList)-1 {
				m.cmdCursor++
			}
		case "q":
			return m, tea.Quit
		case "enter":
			switch commandsList[m.cmdCursor] {
			case "DeviceInformation":
				m.selected = map[int]bool{}
				m.queryCursor = 0
				m.state = stateQueries
			case "DeviceLock":
				m.state = stateWaiting
				return m, tea.Batch(m.enqueue("DeviceLock", map[string]any{}), m.spinner.Tick)
			}
		}
		return m, nil

	case stateQueries:
		switch msg.String() {
		case "up", "k":
			if m.queryCursor > 0 {
				m.queryCursor--
			}
		case "down", "j":
			if m.queryCursor < len(deviceInfoQueries)-1 {
				m.queryCursor++
			}
		case " ", "space":
			m.selected[m.queryCursor] = !m.selected[m.queryCursor]
		case "esc":
			m.state = stateCommand
		case "enter":
			var queries []string
			for i, q := range deviceInfoQueries {
				if m.selected[i] {
					queries = append(queries, q)
				}
			}
			if len(queries) == 0 {
				return m, nil
			}
			m.state = stateWaiting
			fields := map[string]any{"Queries": queries}
			return m, tea.Batch(m.enqueue("DeviceInformation", fields), m.spinner.Tick)
		}
		return m, nil

	case stateResult:
		if msg.String() == "q" {
			return m, tea.Quit
		}
		// any other key: send another command, keeping token + subject
		m.result = nil
		m.err = nil
		m.status = ""
		m.uuid = ""
		m.state = stateCommand
		return m, nil
	}
	return m, nil
}

// fetchDevices lists the enrolled devices from the mdm admin endpoint.
func (m model) fetchDevices() tea.Cmd {
	url := m.url + "/enrollments"
	token := m.token
	client := m.http

	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return errMsg{err}
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		if err != nil {
			return errMsg{err}
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			return errMsg{fmt.Errorf("list devices: %s: %s", resp.Status, strings.TrimSpace(string(b)))}
		}

		var devices []device
		if err := json.NewDecoder(resp.Body).Decode(&devices); err != nil {
			return errMsg{err}
		}
		return devicesMsg{devices: devices}
	}
}

// enqueue POSTs the command to the mdm admin endpoint and returns its UUID.
func (m model) enqueue(requestType string, fields map[string]any) tea.Cmd {
	url := m.url + "/enqueue/" + m.subject
	token := m.token
	client := m.http

	return func() tea.Msg {
		body, err := json.Marshal(map[string]any{"requestType": requestType, "command": fields})
		if err != nil {
			return errMsg{err}
		}

		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return errMsg{err}
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return errMsg{err}
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusAccepted {
			b, _ := io.ReadAll(resp.Body)
			return errMsg{fmt.Errorf("enqueue: %s: %s", resp.Status, strings.TrimSpace(string(b)))}
		}

		var out struct {
			CommandUUID string `json:"commandUUID"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return errMsg{err}
		}
		return enqueuedMsg{uuid: out.CommandUUID}
	}
}

// wait blocks on the command_responded subject for this device until the matching
// CommandUUID arrives or the timeout elapses.
func (m model) wait(uuid string) tea.Cmd {
	nc := m.nc
	subject := "commands." + m.subject + ".responded"
	timeout := m.timeout

	return func() tea.Msg {
		sub, err := nc.SubscribeSync(subject)
		if err != nil {
			return errMsg{err}
		}
		defer sub.Unsubscribe()

		deadline := time.Now().Add(timeout)
		for {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return timeoutMsg{}
			}

			msg, err := sub.NextMsg(remaining)
			if err != nil {
				if errors.Is(err, nats.ErrTimeout) {
					return timeoutMsg{}
				}
				return errMsg{err}
			}

			var evt command.RespondedEvent
			if json.Unmarshal(msg.Data, &evt) != nil {
				continue
			}
			if evt.CommandUUID == uuid {
				return respondedMsg{evt: &evt}
			}
		}
	}
}

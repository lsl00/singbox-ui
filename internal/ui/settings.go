package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"

	"mihomo-tui/internal/app"
)

// drawSettings renders the controller configuration and the kernel actions.
func drawSettings(screen tcell.Screen, x, y, width, height int, state *app.App) {
	connectionHeight := minInt(12, maxInt(height, 1))
	drawBox(screen, x, y, width, connectionHeight, "Connection")
	if connectionHeight >= 9 {
		urlStyle := cellStyle(colorText, colorBackground)
		secretStyle := cellStyle(colorText, colorBackground)
		pathStyle := cellStyle(colorText, colorBackground)
		switch state.SettingsField {
		case 0:
			urlStyle = selectedStyle()
		case 1:
			secretStyle = selectedStyle()
		case 2:
			pathStyle = selectedStyle()
		}
		drawText(screen, x+2, y+1, width-4, "Controller URL", cellStyle(colorMuted, colorBackground))
		drawText(screen, x+2, y+2, width-4, fieldDisplay(state.Config.URL, false,
			state.SettingsField == 0, state.SettingsEditing && state.SettingsField == 0, state.SettingsCursor), urlStyle)
		drawText(screen, x+2, y+4, width-4, "Secret", cellStyle(colorMuted, colorBackground))
		drawText(screen, x+2, y+5, width-4, fieldDisplay(state.Config.Secret, true,
			state.SettingsField == 1, state.SettingsEditing && state.SettingsField == 1, state.SettingsCursor), secretStyle)
		drawText(screen, x+2, y+7, width-4, "Config path (optional)", cellStyle(colorMuted, colorBackground))
		drawText(screen, x+2, y+8, width-4, fieldDisplay(state.ConfigPath, false,
			state.SettingsField == 2, state.SettingsEditing && state.SettingsField == 2, state.SettingsCursor), pathStyle)
	}
	if connectionHeight >= 11 {
		instruction := "Press c to connect with the values above."
		if state.Connected {
			instruction = "Connected. Press d to disconnect or edit then c to reconnect."
		}
		drawText(screen, x+2, y+10, width-4, instruction, cellStyle(colorMuted, colorBackground))
	}

	actionY := y + connectionHeight
	actionHeight := minInt(7, maxInt(y+height-actionY, 0))
	if actionHeight >= 2 {
		drawBox(screen, x, actionY, width, actionHeight, "Kernel")
		lines := []string{
			"R  reload configuration      X  restart kernel",
			"F  flush dns cache           P  flush fake-ip cache",
			fmt.Sprintf("mode: %s   log-level: %s", state.Mode(), logLevelName(state)),
		}
		for index, line := range lines {
			if actionY+1+index >= actionY+actionHeight-1 {
				break
			}
			drawText(screen, x+2, actionY+1+index, width-4, line, cellStyle(colorText, colorBackground))
		}
	}

	aboutY := actionY + actionHeight
	if aboutY < y+height {
		drawBox(screen, x, aboutY, width, y+height-aboutY, "About")
		if y+height-aboutY >= 3 {
			drawText(screen, x+2, aboutY+1, width-4, "mihomo controller client over plain HTTP.",
				cellStyle(colorText, colorBackground))
			drawText(screen, x+2, aboutY+2, width-4, "Config: ~/.config/mihomo-tui/config",
				cellStyle(colorMuted, colorBackground))
		}
	}
}

// fieldDisplay renders an editable field, masking secrets and showing the
// cursor while the field is being edited.
func fieldDisplay(value string, secret, selected, editing bool, cursor int) string {
	if secret {
		value = repeatRunes('*', len([]rune(value)))
	}
	if selected && editing {
		runes := []rune(value)
		position := minInt(maxInt(cursor, 0), len(runes))
		runes = append(runes, 0)
		copy(runes[position+1:], runes[position:])
		runes[position] = '|'
		return string(runes)
	}
	return value
}

func repeatRunes(value rune, count int) string {
	if count <= 0 {
		return ""
	}
	out := make([]rune, count)
	for index := range out {
		out[index] = value
	}
	return string(out)
}

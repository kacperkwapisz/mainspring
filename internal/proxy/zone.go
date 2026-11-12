package proxy

import (
	"sync"
	"time"
)

// SettingTimezone is the IANA timezone (like Europe/Warsaw) that days are
// counted in: Today in the panel, the Meter's days and each key's daily cap.
// Empty until the panel first fills it in from the browser.
const SettingTimezone = "general.timezone"

var zones sync.Map // name → *time.Location

// LoadZone resolves an IANA timezone name. "Local" isn't accepted: on Docker and
// Umbrel it's UTC, which is what the setting exists to avoid.
func LoadZone(name string) (*time.Location, bool) {
	if name == "" || name == "Local" {
		return nil, false
	}
	if loc, ok := zones.Load(name); ok {
		return loc.(*time.Location), true
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, false
	}
	zones.Store(name, loc)
	return loc, true
}

// Zone is the timezone days are counted in: the setting, or UTC until it's set.
func Zone(hub *Hub) *time.Location {
	if hub != nil && hub.Settings != nil {
		name, _ := hub.Settings.Get(SettingTimezone)
		if loc, ok := LoadZone(name); ok {
			return loc
		}
	}
	return time.UTC
}

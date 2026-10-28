package shared

import (
	"fmt"
	"regexp"
	"strings"
)

var usbDeviceID = regexp.MustCompile(`^[0-9a-fA-F]{4}:[0-9a-fA-F]{4}$`)

// ValidateUSBDevices checks the saved selection without probing host hardware.
func (c Config) ValidateUSBDevices() error {
	seen := make(map[string]bool, len(c.USBDevices))
	for _, id := range c.USBDevices {
		if !usbDeviceID.MatchString(id) {
			return fmt.Errorf("USB %q: ожидается VID:PID из четырёх шестнадцатеричных цифр в каждой части, например 2104:0313", id)
		}
		key := strings.ToLower(id)
		if seen[key] {
			return fmt.Errorf("USB %s указан несколько раз", id)
		}
		seen[key] = true
	}
	return nil
}

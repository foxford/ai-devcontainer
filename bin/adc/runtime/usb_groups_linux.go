package runtime

import (
	"errors"
	"fmt"
	"os/user"
	"strconv"
	"strings"
)

// Supplementary groups must also be recorded in /etc/group: Docker exec -u node
// and runuser resolve memberships there instead of inheriting Docker's group-add.
// Device ownership and modes remain under the host's control.
func runtimeUSBGroups(value string, primaryGID int, lookup func(string) (*user.Group, error), run func(string, ...string) error) error {
	gids, err := usbGroupIDs(value, primaryGID)
	if err != nil {
		return err
	}
	if len(gids) == 0 {
		return nil
	}
	for _, gid := range gids {
		if _, err := lookup(gid); err != nil {
			var missing user.UnknownGroupIdError
			if !errors.As(err, &missing) {
				return fmt.Errorf("lookup USB group %s: %w", gid, err)
			}
			if err := run("groupadd", "--gid", gid, "adc-usb-"+gid); err != nil {
				return fmt.Errorf("create USB group %s: %w", gid, err)
			}
		}
	}
	// -a preserves the user's existing supplementary groups.
	if err := run("usermod", "-a", "-G", strings.Join(gids, ","), "node"); err != nil {
		return fmt.Errorf("add USB groups to node: %w", err)
	}
	return nil
}

func usbGroupIDs(value string, primaryGID int) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	var gids []string
	seen := make(map[uint64]bool)
	for _, part := range strings.Split(value, ",") {
		id, err := strconv.ParseUint(part, 10, 32)
		// gid_t(-1) is a sentinel, and root group membership is never needed.
		if err != nil || id == 0 || id == 1<<32-1 {
			return nil, fmt.Errorf("ADC_USB_GIDS contains invalid GID %q; expected 1..4294967294", part)
		}
		if id != uint64(primaryGID) && !seen[id] {
			gids = append(gids, strconv.FormatUint(id, 10))
			seen[id] = true
		}
	}
	return gids, nil
}

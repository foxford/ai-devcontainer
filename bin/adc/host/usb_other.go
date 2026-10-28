//go:build !linux

package host

import "errors"

func statUSBNode(string) (usbNode, error) {
	return usbNode{}, errors.New("USB требует Linux/WSL")
}

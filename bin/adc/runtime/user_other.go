//go:build !linux

package runtime

import "errors"

func runtimeUser() error {
	return errors.New("runtime-serve работает внутри Linux-контейнера ADC")
}

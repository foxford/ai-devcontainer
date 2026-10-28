package shared

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestValidateUSBDevices(t *testing.T) {
	for _, tc := range []struct {
		name string
		ids  []string
		ok   bool
	}{
		{"absent", nil, true},
		{"empty list", []string{}, true},
		{"one device", []string{"2104:0313"}, true},
		{"hex case", []string{"ABcD:EF01", "0000:ffff"}, true},
		{"empty ID", []string{""}, false},
		{"short vendor", []string{"104:0313"}, false},
		{"long product", []string{"2104:00313"}, false},
		{"not hex", []string{"210g:0313"}, false},
		{"prefix", []string{"0x2104:0313"}, false},
		{"missing separator", []string{"21040313"}, false},
		{"space", []string{" 2104:0313"}, false},
		{"newline", []string{"2104:0313\n"}, false},
		{"comma list", []string{"2104:0313,1234:5678"}, false},
		{"duplicate", []string{"2104:0313", "2104:0313"}, false},
		{"duplicate case", []string{"ABCD:ef01", "abcd:EF01"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{USBDevices: tc.ids}
			err := c.ValidateUSBDevices()
			if (err == nil) != tc.ok {
				t.Fatalf("ids=%q err=%v wanted valid=%v", tc.ids, err, tc.ok)
			}
		})
	}
}

func TestLoadValidatesUSBDevicesAndPreservesLegacyConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		ids  []string
		ok   bool
	}{
		{"legacy without USB", nil, true},
		{"selected USB", []string{"2104:0313", "ABCD:ef01"}, true},
		{"invalid saved USB", []string{"2104/0313"}, false},
		{"duplicate saved USB", []string{"ABCD:ef01", "abcd:EF01"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &App{State: filepath.Join(t.TempDir(), "state")}
			want := Config{Version: Schema, Name: "test", Port: DefaultOrcaPort, USBDevices: tc.ids}
			if err := a.Save(want); err != nil {
				t.Fatal(err)
			}
			got, err := a.Load()
			if (err == nil) != tc.ok {
				t.Fatalf("Load() err=%v wanted valid=%v", err, tc.ok)
			}
			if err == nil && !reflect.DeepEqual(got, want) {
				t.Fatalf("Load()=%+v want=%+v", got, want)
			}
		})
	}
}

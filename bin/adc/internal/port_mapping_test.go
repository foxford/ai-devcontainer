package shared

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParsePortMapping(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  PortMapping
		text  string
	}{
		{"3000:3000", PortMapping{"127.0.0.1", 3000, 3000}, "127.0.0.1:3000:3000"},
		{"8080:8000", PortMapping{"127.0.0.1", 8080, 8000}, "127.0.0.1:8080:8000"},
		{"0.0.0.0:3000:80", PortMapping{"0.0.0.0", 3000, 80}, "0.0.0.0:3000:80"},
		{"192.0.2.12:8080:8000", PortMapping{"192.0.2.12", 8080, 8000}, "192.0.2.12:8080:8000"},
		{"1:65535", PortMapping{"127.0.0.1", 1, 65535}, "127.0.0.1:1:65535"},
		{"65535:1", PortMapping{"127.0.0.1", 65535, 1}, "127.0.0.1:65535:1"},
		{"03000:00080", PortMapping{"127.0.0.1", 3000, 80}, "127.0.0.1:3000:80"},
		{"[::1]:3000:80", PortMapping{"::1", 3000, 80}, "[::1]:3000:80"},
		{"[::]:3000:80", PortMapping{"::", 3000, 80}, "[::]:3000:80"},
		{"[2001:0DB8:0000:0000:0000:0000:0000:0001]:3000:80", PortMapping{"2001:db8::1", 3000, 80}, "[2001:db8::1]:3000:80"},
		{"[::ffff:127.0.0.1]:3000:80", PortMapping{"127.0.0.1", 3000, 80}, "127.0.0.1:3000:80"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParsePortMapping(tc.input)
			if err != nil || got != tc.want {
				t.Fatalf("ParsePortMapping(%q)=%+v, %v; want %+v", tc.input, got, err, tc.want)
			}
			if got.String() != tc.text {
				t.Fatalf("String()=%q want %q", got.String(), tc.text)
			}
			roundTrip, err := ParsePortMapping(got.String())
			if err != nil || roundTrip != got {
				t.Fatalf("canonical value did not round trip: %+v, %v", roundTrip, err)
			}
		})
	}
}

func TestParsePortMappingRejectsInvalidInput(t *testing.T) {
	for _, input := range []string{
		"", "3000", ":3000", "3000:", ":3000:80", "127.0.0.1::80", "127.0.0.1:3000:",
		"0:80", "80:0", "65536:80", "80:65536", "99999999999999999999999999:80",
		"-1:80", "+1:80", "3000:-80", "3000:+80", "3.0:80", "abc:80", "3000:http", "３０００:80",
		"3000-3005:80", "3000:80-85", "3000:80/tcp", "3000:80/udp", "3000:80/sctp",
		" 3000:80", "3000:80 ", "3000:\n80", "127.0.0.1: 3000:80", "3000:80\x00",
		"localhost:3000:80", "example.com:3000:80", "127.0.0.999:3000:80", "127.00.0.1:3000:80",
		"::1:3000:80", "[::1:3000:80", "[::1]]:3000:80", "[not-an-ip]:3000:80", "[]:3000:80",
		"[fe80::1%eth0]:3000:80", "[::1]:3000:80:90", "3000:80,8080:8000",
	} {
		t.Run(input, func(t *testing.T) {
			if got, err := ParsePortMapping(input); err == nil {
				t.Fatalf("ParsePortMapping(%q) accepted %+v", input, got)
			}
		})
	}
}

func TestValidatePublishedPorts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ports []PortMapping
		valid bool
	}{
		{"absent", nil, true},
		{"empty", []PortMapping{}, true},
		{"one port", []PortMapping{{"127.0.0.1", 3000, 3000}}, true},
		{"same target", []PortMapping{{"127.0.0.1", 3000, 80}, {"::1", 8080, 80}}, true},
		{"platform target", []PortMapping{{"127.0.0.1", 3000, 6768}, {"::", 8080, 20128}, {"0.0.0.0", 9090, 4242}}, true},
		{"bounds", []PortMapping{{"127.0.0.1", 1, 65535}, {"::1", 65535, 1}}, true},
		{"empty host IP", []PortMapping{{"", 3000, 80}}, false},
		{"hostname", []PortMapping{{"localhost", 3000, 80}}, false},
		{"IPv6 zone", []PortMapping{{"fe80::1%eth0", 3000, 80}}, false},
		{"IPv6 stored with brackets", []PortMapping{{"[::1]", 3000, 80}}, false},
		{"zero host", []PortMapping{{"127.0.0.1", 0, 80}}, false},
		{"large host", []PortMapping{{"127.0.0.1", 65536, 80}}, false},
		{"negative host", []PortMapping{{"127.0.0.1", -1, 80}}, false},
		{"zero target", []PortMapping{{"127.0.0.1", 3000, 0}}, false},
		{"large target", []PortMapping{{"127.0.0.1", 3000, 65536}}, false},
		{"negative target", []PortMapping{{"127.0.0.1", 3000, -1}}, false},
		{"duplicate", []PortMapping{{"127.0.0.1", 3000, 80}, {"127.0.0.1", 3000, 80}}, false},
		{"same host different targets", []PortMapping{{"127.0.0.1", 3000, 80}, {"127.0.0.1", 3000, 8080}}, false},
		{"same host different IPs", []PortMapping{{"127.0.0.1", 3000, 80}, {"192.0.2.12", 3000, 8080}}, false},
		{"same host different families", []PortMapping{{"127.0.0.1", 3000, 80}, {"::1", 3000, 8080}}, false},
		{"Orca conflict", []PortMapping{{"127.0.0.1", DefaultOrcaPort, 80}}, false},
		{"OmniRoute conflict", []PortMapping{{"::", 20128, 80}}, false},
		{"graph conflict", []PortMapping{{"0.0.0.0", 4242, 80}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{Port: DefaultOrcaPort, PublishedPorts: tc.ports}
			if err := c.ValidatePublishedPorts(); (err == nil) != tc.valid {
				t.Fatalf("ValidatePublishedPorts()=%v wanted valid=%v", err, tc.valid)
			}
			if err := c.ValidatePorts(); (err == nil) != tc.valid {
				t.Fatalf("ValidatePorts()=%v wanted valid=%v", err, tc.valid)
			}
		})
	}
}

func TestValidatePortsRejectsBuiltinChangesConflictingWithPublications(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{"Orca", func(c *Config) { c.Port = 3000 }},
		{"OmniRoute", func(c *Config) { c.OmniRoutePort = 3000 }},
		{"graph", func(c *Config) { c.GraphPort = 3000 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{Port: DefaultOrcaPort, PublishedPorts: []PortMapping{{"127.0.0.1", 3000, 80}}}
			if err := c.ValidatePorts(); err != nil {
				t.Fatal(err)
			}
			tc.change(&c)
			if err := c.ValidatePorts(); err == nil || !strings.Contains(err.Error(), "3000") {
				t.Fatalf("conflicting change accepted or unclear error: %v", err)
			}
		})
	}
}

func TestLoadValidatesPublishedPorts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ports []PortMapping
		valid bool
	}{
		{"legacy", nil, true},
		{"configured", []PortMapping{{"127.0.0.1", 3000, 80}, {"::1", 8080, 8000}}, true},
		{"bad address", []PortMapping{{"localhost", 3000, 80}}, false},
		{"bad port", []PortMapping{{"127.0.0.1", 3000, 65536}}, false},
		{"built-in conflict", []PortMapping{{"127.0.0.1", DefaultOrcaPort, 80}}, false},
		{"duplicate", []PortMapping{{"127.0.0.1", 3000, 80}, {"::1", 3000, 8080}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &App{State: t.TempDir()}
			want := Config{Version: Schema, Name: "test", Port: DefaultOrcaPort, USBDevices: []string{"2104:0313"}, PublishedPorts: tc.ports}
			if err := a.Save(want); err != nil {
				t.Fatal(err)
			}
			if tc.ports == nil {
				data, err := os.ReadFile(filepath.Join(a.State, "workspace.json"))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(data), "publishedPorts") {
					t.Fatalf("legacy config acquired publishedPorts: %s", data)
				}
			}
			got, err := a.Load()
			if (err == nil) != tc.valid {
				t.Fatalf("Load()=%v wanted valid=%v", err, tc.valid)
			}
			if err == nil && !reflect.DeepEqual(got, want) {
				t.Fatalf("Load()=%+v want=%+v", got, want)
			}
		})
	}
}

func TestPortMappingStringCanonicalizesSavedIP(t *testing.T) {
	for _, tc := range []struct {
		ip   string
		want string
	}{
		{"2001:0DB8::0001", "[2001:db8::1]:3000:80"},
		{"::ffff:127.0.0.1", "127.0.0.1:3000:80"},
	} {
		p := PortMapping{tc.ip, 3000, 80}
		if p.String() != tc.want {
			t.Errorf("%q: String()=%q want=%q", tc.ip, p.String(), tc.want)
		}
	}
}

package runtime

import (
	"errors"
	"os/user"
	"reflect"
	"strings"
	"testing"
)

func TestRuntimeUSBGroupsRecordsMembershipAndPreservesOtherGroups(t *testing.T) {
	var lookups []string
	lookup := func(gid string) (*user.Group, error) {
		lookups = append(lookups, gid)
		if gid == "46" {
			return &user.Group{Name: "plugdev", Gid: gid}, nil
		}
		return nil, user.UnknownGroupIdError(gid)
	}
	var commands [][]string
	run := func(name string, args ...string) error {
		commands = append(commands, append([]string{name}, args...))
		return nil
	}
	if err := runtimeUSBGroups("46,1000,3000,046,3000", 1000, lookup, run); err != nil {
		t.Fatal(err)
	}
	if want := []string{"46", "3000"}; !reflect.DeepEqual(lookups, want) {
		t.Fatalf("lookups = %v, want %v", lookups, want)
	}
	want := [][]string{
		{"groupadd", "--gid", "3000", "adc-usb-3000"},
		{"usermod", "-a", "-G", "46,3000", "node"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %v, want %v", commands, want)
	}
}

func TestRuntimeUSBGroupsDoesNothingWithoutSupplementaryGroups(t *testing.T) {
	for _, value := range []string{"", "1000,01000"} {
		t.Run(value, func(t *testing.T) {
			lookup := func(string) (*user.Group, error) {
				t.Fatal("unexpected group lookup")
				return nil, nil
			}
			run := func(string, ...string) error {
				t.Fatal("unexpected command")
				return nil
			}
			if err := runtimeUSBGroups(value, 1000, lookup, run); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRuntimeUSBGroupsRejectsInvalidListBeforeChanges(t *testing.T) {
	for _, value := range []string{"0", "46,0", "46,-1", "46,", ",46", "46,,3000", " 46", "46 ", "+46", "4294967295", "4294967296", "plugdev", "46,27;id"} {
		t.Run(value, func(t *testing.T) {
			lookup := func(string) (*user.Group, error) {
				t.Fatal("invalid list caused a group lookup")
				return nil, nil
			}
			run := func(string, ...string) error {
				t.Fatal("invalid list caused a command")
				return nil
			}
			if err := runtimeUSBGroups(value, 1000, lookup, run); err == nil || !strings.Contains(err.Error(), "ADC_USB_GIDS") {
				t.Fatalf("expected ADC_USB_GIDS validation error, got %v", err)
			}
		})
	}
}

func TestRuntimeUSBGroupsPropagatesLookupAndCommandFailures(t *testing.T) {
	problem := errors.New("unavailable")
	t.Run("lookup", func(t *testing.T) {
		lookup := func(string) (*user.Group, error) { return nil, problem }
		run := func(string, ...string) error {
			t.Fatal("lookup failure caused a command")
			return nil
		}
		if err := runtimeUSBGroups("46", 1000, lookup, run); !errors.Is(err, problem) {
			t.Fatalf("expected lookup error, got %v", err)
		}
	})
	for _, command := range []string{"groupadd", "usermod"} {
		t.Run(command, func(t *testing.T) {
			lookup := func(gid string) (*user.Group, error) { return nil, user.UnknownGroupIdError(gid) }
			var commands []string
			run := func(name string, _ ...string) error {
				commands = append(commands, name)
				if name == command {
					return problem
				}
				return nil
			}
			if err := runtimeUSBGroups("46", 1000, lookup, run); !errors.Is(err, problem) {
				t.Fatalf("expected command error, got %v", err)
			}
			if commands[len(commands)-1] != command {
				t.Fatalf("commands continued after failure: %v", commands)
			}
		})
	}
}

package shared

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// PortMapping publishes one TCP port of the shared runtime on the host.
type PortMapping struct {
	HostIP        string `json:"hostIP"`
	HostPort      int    `json:"hostPort"`
	ContainerPort int    `json:"containerPort"`
}

// ParsePortMapping accepts HOST:CONTAINER or IP:HOST:CONTAINER, with IPv6
// literals enclosed in brackets. Omitting IP binds only to IPv4 loopback.
func ParsePortMapping(value string) (PortMapping, error) {
	var mapping PortMapping
	invalidFormat := func() (PortMapping, error) {
		return mapping, fmt.Errorf("публикация порта %q: ожидается HOST:CONTAINER или IP:HOST:CONTAINER, например 3000:3000; поддерживается только TCP", value)
	}
	separator := strings.LastIndexByte(value, ':')
	if separator < 1 {
		return invalidFormat()
	}
	host, container := value[:separator], value[separator+1:]
	hostIP, hostPort := "127.0.0.1", host
	if strings.ContainsRune(host, ':') {
		var err error
		hostIP, hostPort, err = net.SplitHostPort(host)
		if err != nil {
			return invalidFormat()
		}
	}
	addr, err := parsePublishIP(hostIP)
	if err != nil {
		return mapping, fmt.Errorf("публикация порта %q: %w", value, err)
	}
	mapping.HostIP = addr.Unmap().String()
	mapping.HostPort, err = parsePublishPort(hostPort)
	if err != nil {
		return mapping, fmt.Errorf("публикация порта %q: порт хоста %w", value, err)
	}
	mapping.ContainerPort, err = parsePublishPort(container)
	if err != nil {
		return mapping, fmt.Errorf("публикация порта %q: порт контейнера %w", value, err)
	}
	return mapping, nil
}

// String returns Docker's canonical publish argument with an explicit IP.
func (p PortMapping) String() string {
	host := p.HostIP
	if addr, err := parsePublishIP(host); err == nil {
		host = addr.Unmap().String()
	}
	return net.JoinHostPort(host, strconv.Itoa(p.HostPort)) + ":" + strconv.Itoa(p.ContainerPort)
}

// ValidatePublishedPorts checks persisted settings without opening sockets.
// Distinct host ports keep conflicts unambiguous across IPv4/IPv6 and wildcard
// bindings, including the workspace's built-in services.
func (c Config) ValidatePublishedPorts() error {
	seen := map[int]string{
		c.Port:          "Orca",
		c.GatewayPort(): "OmniRoute",
		c.PreviewPort(): "граф",
	}
	for _, p := range c.PublishedPorts {
		if _, err := parsePublishIP(p.HostIP); err != nil {
			return fmt.Errorf("публикация порта %q: %w", p.String(), err)
		}
		if p.HostPort < 1 || p.HostPort > 65535 || p.ContainerPort < 1 || p.ContainerPort > 65535 {
			return fmt.Errorf("публикация порта %q: порты хоста и контейнера должны быть числами от 1 до 65535", p.String())
		}
		if previous, ok := seen[p.HostPort]; ok {
			return fmt.Errorf("порт хоста %d уже используется (%s); номера портов хоста должны различаться независимо от IP", p.HostPort, previous)
		}
		seen[p.HostPort] = p.String()
	}
	return nil
}

func parsePublishIP(value string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(value)
	if err != nil || addr.Zone() != "" {
		return netip.Addr{}, fmt.Errorf("адрес хоста %q должен быть IPv4 или IPv6 без имени хоста и зоны интерфейса", value)
	}
	return addr, nil
}

func parsePublishPort(value string) (int, error) {
	if value == "" || strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) != -1 {
		return 0, fmt.Errorf("%q должен быть числом от 1 до 65535", value)
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("%q должен быть числом от 1 до 65535", value)
	}
	return port, nil
}

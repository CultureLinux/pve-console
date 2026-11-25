package proxmox

import (
	"context"
	"errors"
	"testing"

	"pvc/internal/command"
)

func TestUsableIPv4(t *testing.T) {
	tests := []struct {
		name, iface, address, want string
	}{
		{"plain", "eth0", "192.168.1.201", "192.168.1.201"},
		{"with prefix", "eth0", "10.0.0.5/24", "10.0.0.5"},
		{"loopback", "eth0", "127.0.0.1", ""},
		{"loopback interface", "lo", "192.168.9.9", ""},
		{"link local", "eth0", "169.254.3.4", ""},
		{"ipv6", "eth0", "2a02:8428::1", ""},
		{"docker bridge", "docker0", "172.17.0.1", ""},
		{"compose bridge", "br-1a2b3c", "172.18.0.1", ""},
		{"garbage", "eth0", "not an ip", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := usableIPv4(tt.iface, tt.address)
			if got != tt.want || ok != (tt.want != "") {
				t.Fatalf("usableIPv4(%q, %q) = %q, %v; want %q", tt.iface, tt.address, got, ok, tt.want)
			}
		})
	}
}

const agentInterfaces = `{"result":[
 {"name":"lo","ip-addresses":[{"ip-address":"127.0.0.1","ip-address-type":"ipv4","prefix":8}]},
 {"name":"docker0","ip-addresses":[{"ip-address":"172.17.0.1","ip-address-type":"ipv4","prefix":16}]},
 {"name":"eth0","ip-addresses":[{"ip-address":"fe80::1","ip-address-type":"ipv6","prefix":64},{"ip-address":"192.168.1.201","ip-address-type":"ipv4","prefix":24}]}]}`

func TestGuestNetworkOfAVMWithAnAgent(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{
		"pvesh get /nodes/pve/qemu/200/config --output-format json":                       {Stdout: []byte(`{"agent":"enabled=1,type=virtio"}`)},
		"pvesh get /nodes/pve/qemu/200/agent/network-get-interfaces --output-format json": {Stdout: []byte(agentInterfaces)},
	}}
	info, err := Client{Runner: runner, Node: "pve"}.GuestNetwork(context.Background(), 200, GuestTypeQEMU)
	if err != nil || !info.AgentEnabled || !info.AgentRunning || info.IPv4 != "192.168.1.201" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
}

func TestGuestNetworkWithTheAgentDisabledAsksNothingElse(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{
		"pvesh get /nodes/pve/qemu/200/config --output-format json": {Stdout: []byte(`{"agent":"enabled=0"}`)},
	}}
	info, err := Client{Runner: runner, Node: "pve"}.GuestNetwork(context.Background(), 200, GuestTypeQEMU)
	if err != nil || info.AgentEnabled || info.AgentRunning || len(runner.Calls) != 1 {
		t.Fatalf("info=%+v err=%v calls=%v", info, err, runner.Calls)
	}
}

func TestGuestNetworkWithASilentAgentIsNotAnError(t *testing.T) {
	runner := &command.MockRunner{
		Responses: map[string]command.Result{
			"pvesh get /nodes/pve/qemu/200/config --output-format json": {Stdout: []byte(`{"agent":"1"}`)},
		},
		Errors: map[string]error{
			"pvesh get /nodes/pve/qemu/200/agent/network-get-interfaces --output-format json": errors.New("QEMU guest agent is not running"),
		},
	}
	info, err := Client{Runner: runner, Node: "pve"}.GuestNetwork(context.Background(), 200, GuestTypeQEMU)
	if err != nil || !info.AgentEnabled || info.AgentRunning || info.IPv4 != "" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
}

func TestGuestNetworkOfAContainer(t *testing.T) {
	runner := &command.MockRunner{Responses: map[string]command.Result{
		"pvesh get /nodes/pve/lxc/400/interfaces --output-format json": {Stdout: []byte(`[{"name":"lo","inet":"127.0.0.1/8"},{"name":"eth0","inet":"10.0.0.5/24","inet6":"fe80::1/64"}]`)},
	}}
	info, err := Client{Runner: runner, Node: "pve"}.GuestNetwork(context.Background(), 400, GuestTypeLXC)
	if err != nil || info.AgentEnabled || info.IPv4 != "10.0.0.5" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
}

func TestGuestNetworkReportsAFailedRead(t *testing.T) {
	runner := &command.MockRunner{Errors: map[string]error{
		"pvesh get /nodes/pve/qemu/200/config --output-format json": errors.New("boom"),
	}}
	if _, err := (Client{Runner: runner, Node: "pve"}).GuestNetwork(context.Background(), 200, GuestTypeQEMU); err == nil {
		t.Fatal("expected an error")
	}
}

package ui

import (
	"testing"

	"pvc/internal/proxmox"
)

func TestFilteredGuests(t *testing.T) {
	model := New(proxmox.Client{})
	model.data.guests = []proxmox.Guest{
		{VMID: 302, Name: "minio", Status: "running"},
		{VMID: 300, Name: "wordpress", Status: "stopped"},
		{VMID: 301, Name: "worker", Status: "running"},
	}
	model.filter = "running"
	model.query = "wo"
	model.sortBy = "name"

	guests := model.filteredGuests()
	if len(guests) != 1 {
		t.Fatalf("expected 1 guest, got %d", len(guests))
	}
	if guests[0].VMID != 301 {
		t.Fatalf("expected worker guest, got %#v", guests[0])
	}
}

func TestNextFilter(t *testing.T) {
	model := New(proxmox.Client{})

	model.nextFilter()
	if model.filter != "running" {
		t.Fatalf("expected running, got %q", model.filter)
	}
	model.nextFilter()
	if model.filter != "stopped" {
		t.Fatalf("expected stopped, got %q", model.filter)
	}
	model.nextFilter()
	if model.filter != "all" {
		t.Fatalf("expected all, got %q", model.filter)
	}
}

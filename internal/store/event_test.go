package store

import "testing"

// Events carry their device's current name, and lose it with the device.
func TestListEventsCarriesDeviceName(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	devID, err := s.CreateDevice(ctx, Device{Name: "nas", Kind: "server", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	s.AddEvent(ctx, "online", &devID, "nas is online")
	s.AddEvent(ctx, "scan_error", nil, "subnet 10.0.0.0/24: boom")

	evs, err := s.ListEvents(ctx, 10)
	if err != nil || len(evs) != 2 {
		t.Fatalf("list: %+v err=%v", evs, err)
	}
	if evs[0].DeviceName != nil || evs[1].DeviceName == nil || *evs[1].DeviceName != "nas" {
		t.Fatalf("names: %+v", evs)
	}
	if devEvs, err := s.ListDeviceEvents(ctx, devID, 10); err != nil || len(devEvs) != 1 || devEvs[0].DeviceName == nil {
		t.Fatalf("device events: %+v err=%v", devEvs, err)
	}

	if err := s.DeleteDevice(ctx, devID); err != nil {
		t.Fatal(err)
	}
	evs, _ = s.ListEvents(ctx, 10)
	if evs[1].DeviceID != nil || evs[1].DeviceName != nil {
		t.Fatalf("after delete: %+v", evs[1])
	}
}

func TestDeviceNames(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	a, _ := s.CreateDevice(ctx, Device{Name: "nas", Kind: "server", Source: "manual"})
	b, _ := s.CreateDevice(ctx, Device{Name: "tv", Kind: "other", Source: "manual"})
	got, err := s.DeviceNames(ctx, []int64{a, b, 999})
	if err != nil || len(got) != 2 || got[a] != "nas" || got[b] != "tv" {
		t.Fatalf("got %v err=%v", got, err)
	}
	if got, err := s.DeviceNames(ctx, nil); err != nil || len(got) != 0 {
		t.Fatalf("empty: %v err=%v", got, err)
	}
}

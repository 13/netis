package autofill

import (
	"reflect"
	"testing"

	"netis/internal/store"
)

func h(source, field, value string, conf int) store.Hint {
	return store.Hint{Source: source, Field: field, Value: value, Confidence: conf}
}

func TestResolvePicksHighestThenRank(t *testing.T) {
	fields, tags := Resolve([]store.Hint{
		h("oui", "vendor", "Apple", 90),
		h("hostname", "vendor", "Samsung", 70),
		h("hostname", "kind", "phone", 70),
		h("ports", "kind", "printer", 70), // tie: ports outranks hostname
		h("oui", "kind", "iot", 40),       // below threshold
		h("hostname", "tag", "media", 60),
		h("ports", "tag", "media", 50),
		h("ports", "tag", "mqtt", 49), // below threshold
	})
	if fields["vendor"].Value != "Apple" || fields["kind"].Value != "printer" || fields["kind"].Source != "ports" {
		t.Fatalf("fields = %+v", fields)
	}
	if len(tags) != 1 || tags[0].Value != "media" || tags[0].Confidence != 60 {
		t.Fatalf("tags = %+v", tags)
	}
	f, _ := Resolve([]store.Hint{h("oui", "kind", "iot", 40)})
	if len(f) != 0 {
		t.Fatalf("low hint resolved: %+v", f)
	}
}

func TestPlaceholderName(t *testing.T) {
	for name, want := range map[string]bool{
		"unknown-bc:24:11:00:00:01":  true,
		"unknown-10.0.0.9":           true,
		"private-da:a1:19:00:00:01":  true,
		"pihole-aa:bb:cc:dd:ee:ff":   true,
		"adguard-aa:bb:cc:dd:ee:ff":  true,
		"opnsense-aa:bb:cc:dd:ee:ff": true,
		"printer":                    false,
		"unknown-thing":              false,
	} {
		if got := IsPlaceholderName(name); got != want {
			t.Errorf("IsPlaceholderName(%q) = %v", name, got)
		}
	}
}

func cand(v, src string) Candidate { return Candidate{Value: v, Source: src, Confidence: 80} }

func TestDecideRules(t *testing.T) {
	applied := func(f, v string) store.AutofillRecord {
		return store.AutofillRecord{Field: f, Value: v, Source: "hostname", State: store.AutofillApplied}
	}
	owned := func(f, v string) store.AutofillRecord {
		return store.AutofillRecord{Field: f, Value: v, State: store.AutofillOwned}
	}
	cases := []struct {
		name   string
		dev    store.Device
		tags   []string
		recs   []store.AutofillRecord
		fields map[string]Candidate
		tagC   []Candidate
		want   store.AutofillChanges
	}{
		{
			name:   "empty field is filled",
			dev:    store.Device{Kind: "other"},
			fields: map[string]Candidate{"vendor": cand("Brother", "oui"), "kind": cand("printer", "hostname")},
			want: store.AutofillChanges{Writes: []store.AutofillWrite{
				{Field: "vendor", Value: "Brother", Source: "oui", Expect: ""},
				{Field: "kind", Value: "printer", Source: "hostname", Expect: "other"},
			}},
		},
		{
			name:   "owned field is skipped",
			dev:    store.Device{Kind: "other", Vendor: ""},
			recs:   []store.AutofillRecord{owned("vendor", "Brother")},
			fields: map[string]Candidate{"vendor": cand("Brother", "oui")},
		},
		{
			name:   "person changed an applied value: becomes owned",
			dev:    store.Device{Kind: "other", Vendor: "Mine"},
			recs:   []store.AutofillRecord{applied("vendor", "Brother")},
			fields: map[string]Candidate{"vendor": cand("Brother", "oui")},
			want:   store.AutofillChanges{Own: []string{"vendor"}},
		},
		{
			name: "person cleared an applied value: becomes owned",
			dev:  store.Device{Kind: "other"},
			recs: []store.AutofillRecord{applied("vendor", "Brother")},
			want: store.AutofillChanges{Own: []string{"vendor"}},
		},
		{
			name:   "better source replaces applied value while unreviewed",
			dev:    store.Device{Kind: "phone"},
			recs:   []store.AutofillRecord{applied("kind", "phone")},
			fields: map[string]Candidate{"kind": cand("computer", "mdns")},
			want: store.AutofillChanges{Writes: []store.AutofillWrite{
				{Field: "kind", Value: "computer", Source: "mdns", Expect: "phone", Unreviewed: true},
			}},
		},
		{
			name:   "reviewed device keeps applied value",
			dev:    store.Device{Kind: "phone", Reviewed: true},
			recs:   []store.AutofillRecord{applied("kind", "phone")},
			fields: map[string]Candidate{"kind": cand("computer", "mdns")},
		},
		{
			name:   "value set by someone else is kept",
			dev:    store.Device{Kind: "vm", Vendor: "Dell"},
			fields: map[string]Candidate{"vendor": cand("Intel", "oui"), "kind": cand("computer", "hostname")},
		},
		{
			name:   "placeholder name on unreviewed device is filled",
			dev:    store.Device{Kind: "other", Name: "unknown-10.0.0.9"},
			fields: map[string]Candidate{"name": cand("Living room TV", "mdns")},
			want: store.AutofillChanges{Writes: []store.AutofillWrite{
				{Field: "name", Value: "Living room TV", Source: "mdns", Expect: "unknown-10.0.0.9", Unreviewed: true},
			}},
		},
		{
			name:   "placeholder name on reviewed device is kept",
			dev:    store.Device{Kind: "other", Name: "unknown-10.0.0.9", Reviewed: true},
			fields: map[string]Candidate{"name": cand("Living room TV", "mdns")},
		},
		{
			name: "tags: add new, skip present, removed becomes owned, owned skipped",
			dev:  store.Device{Kind: "other"},
			tags: []string{"media", "mine"},
			recs: []store.AutofillRecord{applied("tag:nas", "nas"), owned("tag:camera", "camera")},
			tagC: []Candidate{cand("media", "hostname"), cand("smart-home", "ports"), cand("nas", "hostname"), cand("camera", "ports")},
			want: store.AutofillChanges{
				Tags: []store.AutofillTag{{Name: "smart-home", Source: "ports"}},
				Own:  []string{"tag:nas"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := decide(c.dev, c.tags, c.recs, c.fields, c.tagC)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("decide =\n %+v\nwant\n %+v", got, c.want)
			}
		})
	}
}

func TestExplain(t *testing.T) {
	d := store.Device{Kind: "printer", Vendor: "Mine", Model: "HL"}
	recs := []store.AutofillRecord{
		{Field: "kind", Value: "printer", Source: "hostname", State: store.AutofillApplied},
		{Field: "vendor", Value: "Brother", State: store.AutofillOwned},
	}
	hints := []store.Hint{
		h("hostname", "kind", "printer", 80),
		h("oui", "kind", "iot", 40),
		h("ports", "kind", "server", 60),
		h("oui", "vendor", "Brother", 90),
		h("hostname", "model", "HL-L2350", 60),
	}
	got := map[string]string{}
	for _, e := range Explain(d, nil, recs, hints) {
		got[e.Source+"/"+e.Field] = e.Status
	}
	want := map[string]string{
		"hostname/kind":  StatusApplied,
		"oui/kind":       StatusLow,
		"ports/kind":     StatusOutranked,
		"oui/vendor":     StatusOwned,
		"hostname/model": StatusKept,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Explain = %v, want %v", got, want)
	}
}

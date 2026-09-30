package store

import (
	"database/sql"
	"errors"
	"testing"
)

func TestListTagsWithCounts(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		a, _ := s.CreateDevice(ctx, Device{Name: "a", Kind: "other", Source: "manual"})
		b, _ := s.CreateDevice(ctx, Device{Name: "b", Kind: "other", Source: "manual"})
		if err := s.SetDeviceTags(ctx, a, []string{"nas", "lab"}); err != nil {
			t.Fatal(err)
		}
		if err := s.SetDeviceTags(ctx, b, []string{"nas"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateTag(ctx, "unused", "teal"); err != nil {
			t.Fatal(err)
		}
		got, err := s.ListTagsWithCounts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]int{"lab": 1, "nas": 2, "unused": 0}
		if len(got) != 3 {
			t.Fatalf("got %+v", got)
		}
		names := []string{got[0].Name, got[1].Name, got[2].Name}
		if names[0] != "lab" || names[1] != "nas" || names[2] != "unused" {
			t.Fatalf("order = %v", names)
		}
		for _, tc := range got {
			if tc.Devices != want[tc.Name] {
				t.Errorf("%s: devices = %d, want %d", tc.Name, tc.Devices, want[tc.Name])
			}
			if tc.ID == 0 {
				t.Errorf("%s: zero id", tc.Name)
			}
		}
		if got[2].Color != "teal" {
			t.Errorf("unused colour = %q", got[2].Color)
		}
	})
}

func tagByName(t *testing.T, s *Store, name string) (TagCount, bool) {
	t.Helper()
	tags, err := s.ListTagsWithCounts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range tags {
		if tc.Name == name {
			return tc, true
		}
	}
	return TagCount{}, false
}

func TestSetTagColor(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		id, _ := s.CreateTag(t.Context(), "nas", "")
		if err := s.SetTagColor(t.Context(), id, "violet"); err != nil {
			t.Fatal(err)
		}
		tc, _ := tagByName(t, s, "nas")
		if tc.Color != "violet" {
			t.Fatalf("colour = %q", tc.Color)
		}
		if err := s.SetTagColor(t.Context(), id, ""); err != nil {
			t.Fatal(err)
		}
		tc, _ = tagByName(t, s, "nas")
		if tc.Color != "" {
			t.Fatalf("colour = %q, want auto", tc.Color)
		}
		if err := s.SetTagColor(t.Context(), id+99, "sky"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("missing tag: err = %v, want sql.ErrNoRows", err)
		}
	})
}

func TestRenameTagPlain(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		d, _ := s.CreateDevice(ctx, Device{Name: "a", Kind: "other", Source: "manual"})
		s.SetDeviceTags(ctx, d, []string{"media"})
		tc, _ := tagByName(t, s, "media")
		s.SetTagColor(ctx, tc.ID, "pink")

		merged, err := s.RenameTag(ctx, tc.ID, "video")
		if err != nil {
			t.Fatal(err)
		}
		if merged != 0 {
			t.Fatalf("mergedInto = %d, want 0", merged)
		}
		if _, ok := tagByName(t, s, "media"); ok {
			t.Fatal("old name still listed")
		}
		got, ok := tagByName(t, s, "video")
		if !ok || got.ID != tc.ID || got.Color != "pink" || got.Devices != 1 {
			t.Fatalf("renamed = %+v ok=%v", got, ok)
		}

		// Renaming to its own name is a no-op.
		merged, err = s.RenameTag(ctx, tc.ID, "video")
		if err != nil || merged != 0 {
			t.Fatalf("self rename: merged=%d err=%v", merged, err)
		}
		if _, err := s.RenameTag(ctx, tc.ID+99, "x"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("missing tag: err = %v, want sql.ErrNoRows", err)
		}
	})
}

func TestRenameTagMerge(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		a, _ := s.CreateDevice(ctx, Device{Name: "a", Kind: "other", Source: "manual"})
		b, _ := s.CreateDevice(ctx, Device{Name: "b", Kind: "other", Source: "manual"})
		c, _ := s.CreateDevice(ctx, Device{Name: "c", Kind: "other", Source: "manual"})
		s.SetDeviceTags(ctx, a, []string{"nas", "storage"}) // on both
		s.SetDeviceTags(ctx, b, []string{"storage"})
		s.SetDeviceTags(ctx, c, []string{"nas"})
		nas, _ := tagByName(t, s, "nas")
		storage, _ := tagByName(t, s, "storage")
		s.SetTagColor(ctx, nas.ID, "teal")
		s.SetTagColor(ctx, storage.ID, "pink")

		merged, err := s.RenameTag(ctx, storage.ID, "nas")
		if err != nil {
			t.Fatal(err)
		}
		if merged != nas.ID {
			t.Fatalf("mergedInto = %d, want %d", merged, nas.ID)
		}
		if _, ok := tagByName(t, s, "storage"); ok {
			t.Fatal("old tag still exists")
		}
		got, _ := tagByName(t, s, "nas")
		if got.Devices != 3 || got.Color != "teal" {
			t.Fatalf("survivor = %+v, want 3 devices and teal", got)
		}
		tags, _ := s.DeviceTags(ctx, a)
		if len(tags) != 1 || tags[0].Name != "nas" {
			t.Fatalf("device a tags = %+v", tags)
		}
	})
}

func TestDeleteTag(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		a, _ := s.CreateDevice(ctx, Device{Name: "a", Kind: "other", Source: "manual"})
		s.SetDeviceTags(ctx, a, []string{"media", "lab"})
		media, _ := tagByName(t, s, "media")
		if err := s.DeleteTag(ctx, media.ID); err != nil {
			t.Fatal(err)
		}
		if _, ok := tagByName(t, s, "media"); ok {
			t.Fatal("tag still listed")
		}
		tags, _ := s.DeviceTags(ctx, a)
		if len(tags) != 1 || tags[0].Name != "lab" {
			t.Fatalf("device tags = %+v", tags)
		}
		if err := s.DeleteTag(ctx, media.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("second delete: err = %v, want sql.ErrNoRows", err)
		}
	})
}

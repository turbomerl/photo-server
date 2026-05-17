package store

import "testing"

func TestUpsertSessionCreateAndNamePreservation(t *testing.T) {
	s := openTemp(t)

	if err := s.UpsertSession("sess-a", "Aunt Sue"); err != nil {
		t.Fatalf("UpsertSession create: %v", err)
	}
	got, ok, err := s.GetSession("sess-a")
	if err != nil || !ok {
		t.Fatalf("GetSession: ok=%v err=%v", ok, err)
	}
	if got.DisplayName != "Aunt Sue" {
		t.Errorf("DisplayName = %q, want %q", got.DisplayName, "Aunt Sue")
	}
	if got.CreatedAt.IsZero() || got.LastSeenAt.IsZero() {
		t.Errorf("timestamps not set: %+v", got)
	}

	// Empty name on re-upsert must NOT wipe the stored name.
	if err := s.UpsertSession("sess-a", ""); err != nil {
		t.Fatalf("UpsertSession empty: %v", err)
	}
	got, _, _ = s.GetSession("sess-a")
	if got.DisplayName != "Aunt Sue" {
		t.Errorf("name lost on empty upsert: %q", got.DisplayName)
	}

	// A new non-empty name overwrites.
	if err := s.UpsertSession("sess-a", "Auntie Susan"); err != nil {
		t.Fatalf("UpsertSession rename: %v", err)
	}
	got, _, _ = s.GetSession("sess-a")
	if got.DisplayName != "Auntie Susan" {
		t.Errorf("DisplayName = %q, want %q", got.DisplayName, "Auntie Susan")
	}
}

func TestGetSessionMissing(t *testing.T) {
	s := openTemp(t)
	if _, ok, err := s.GetSession("ghost"); ok || err != nil {
		t.Fatalf("missing session: ok=%v err=%v", ok, err)
	}
}

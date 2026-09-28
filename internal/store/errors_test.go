package store

import (
	"context"
	"errors"
	"testing"
)

func TestConformanceConstraintErrorsAreClassified(t *testing.T) {
	eachDialect(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		if _, err := s.CreateSubnet(ctx, Subnet{CIDR: "10.9.0.0/24", Kind: "lan", ScanIntervalSec: 60}); err != nil {
			t.Fatal(err)
		}
		_, err := s.CreateSubnet(ctx, Subnet{CIDR: "10.9.0.0/24", Kind: "lan", ScanIntervalSec: 60})
		if !IsUniqueViolation(err) || IsForeignKeyViolation(err) {
			t.Errorf("duplicate CIDR: unique=%v fk=%v err=%v",
				IsUniqueViolation(err), IsForeignKeyViolation(err), err)
		}

		missing := int64(999)
		_, err = s.CreateDevice(ctx, Device{Name: "orphan", Kind: "other", Source: "manual",
			ParentDeviceID: &missing})
		if !IsForeignKeyViolation(err) || IsUniqueViolation(err) {
			t.Errorf("bad parent: unique=%v fk=%v err=%v",
				IsUniqueViolation(err), IsForeignKeyViolation(err), err)
		}

		other := errors.New("boom")
		if IsUniqueViolation(other) || IsForeignKeyViolation(other) || IsUniqueViolation(nil) {
			t.Error("a plain error must not classify as a constraint violation")
		}
	})
}

package db

import (
	"flag"
	"reflect"
	"sort"
	"testing"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/urfave/cli"
)

func TestStore_AttesterSlashingNilBucket(t *testing.T) {
	app := cli.NewApp()
	set := flag.NewFlagSet("test", 0)
	ctx := cli.NewContext(app, set, nil)
	db := SetupSlasherDB(t, ctx)
	defer TeardownSlasherDB(t, db)
	as := &ethpb.AttesterSlashing{Attestation_1: &ethpb.IndexedAttestation{Signature: []byte("hello")}}
	has, _, err := db.HasAttesterSlashing(as)
	if err != nil {
		t.Fatalf("HasAttesterSlashing should not return error: %v", err)
	}
	if has {
		t.Fatal("HasAttesterSlashing should return false")
	}

	p, err := db.AttesterSlashings(SlashingStatus(Active))
	if err != nil {
		t.Fatalf("Failed to get attester slashing: %v", err)
	}
	if p == nil || len(p) != 0 {
		t.Fatalf("Get should return empty attester slashing array for a non existent key")
	}
}

func TestStore_SaveAttesterSlashing(t *testing.T) {
	app := cli.NewApp()
	set := flag.NewFlagSet("test", 0)
	ctx := cli.NewContext(app, set, nil)
	db := SetupSlasherDB(t, ctx)
	defer TeardownSlasherDB(t, db)
	tests := []struct {
		ss SlashingStatus
		as *ethpb.AttesterSlashing
	}{
		{
			ss: Active,
			as: &ethpb.AttesterSlashing{Attestation_1: &ethpb.IndexedAttestation{Signature: []byte("hello")}},
		},
		{
			ss: Included,
			as: &ethpb.AttesterSlashing{Attestation_1: &ethpb.IndexedAttestation{Signature: []byte("hello2")}},
		},
		{
			ss: Reverted,
			as: &ethpb.AttesterSlashing{Attestation_1: &ethpb.IndexedAttestation{Signature: []byte("hello3")}},
		},
	}

	for _, tt := range tests {
		err := db.SaveAttesterSlashing(tt.ss, tt.as)
		if err != nil {
			t.Fatalf("save attester slashing failed: %v", err)
		}

		attesterSlashings, err := db.AttesterSlashings(tt.ss)
		if err != nil {
			t.Fatalf("failed to get attester slashings: %v", err)
		}

		if attesterSlashings == nil || !reflect.DeepEqual(attesterSlashings[0], tt.as) {
			t.Fatalf("attester slashing: %v should be part of attester slashings response: %v", tt.as, attesterSlashings)
		}
	}

}

func TestStore_SaveAttesterSlashings(t *testing.T) {
	app := cli.NewApp()
	set := flag.NewFlagSet("test", 0)
	ctx := cli.NewContext(app, set, nil)
	db := SetupSlasherDB(t, ctx)
	defer TeardownSlasherDB(t, db)
	as := []*ethpb.AttesterSlashing{
		{Attestation_1: &ethpb.IndexedAttestation{Signature: []byte("1")}},
		{Attestation_1: &ethpb.IndexedAttestation{Signature: []byte("2")}},
		{Attestation_1: &ethpb.IndexedAttestation{Signature: []byte("3")}},
	}
	err := db.SaveAttesterSlashings(Active, as)
	if err != nil {
		t.Fatalf("save attester slashing failed: %v", err)
	}
	attesterSlashings, err := db.AttesterSlashings(Active)
	if err != nil {
		t.Fatalf("failed to get attester slashings: %v", err)
	}
	sort.SliceStable(attesterSlashings, func(i, j int) bool {
		return attesterSlashings[i].Attestation_1.Signature[0] < attesterSlashings[j].Attestation_1.Signature[0]
	})
	if attesterSlashings == nil || !reflect.DeepEqual(attesterSlashings, as) {
		t.Fatalf("Attester slashing: %v should be part of attester slashings response: %v", as, attesterSlashings)
	}
}

func TestStore_UpdateAttesterSlashingStatus(t *testing.T) {
	app := cli.NewApp()
	set := flag.NewFlagSet("test", 0)
	ctx := cli.NewContext(app, set, nil)
	db := SetupSlasherDB(t, ctx)
	defer TeardownSlasherDB(t, db)
	tests := []struct {
		ss SlashingStatus
		as *ethpb.AttesterSlashing
	}{
		{
			ss: Active,
			as: &ethpb.AttesterSlashing{Attestation_1: &ethpb.IndexedAttestation{Signature: []byte("hello")}},
		},
		{
			ss: Active,
			as: &ethpb.AttesterSlashing{Attestation_1: &ethpb.IndexedAttestation{Signature: []byte("hello2")}},
		},
		{
			ss: Active,
			as: &ethpb.AttesterSlashing{Attestation_1: &ethpb.IndexedAttestation{Signature: []byte("hello3")}},
		},
	}

	for _, tt := range tests {
		err := db.SaveAttesterSlashing(tt.ss, tt.as)
		if err != nil {
			t.Fatalf("save attester slashing failed: %v", err)
		}
	}

	for _, tt := range tests {
		has, st, err := db.HasAttesterSlashing(tt.as)
		if err != nil {
			t.Fatalf("Failed to get attester slashing: %v", err)
		}
		if !has {
			t.Fatalf("Failed to find attester slashing: %v", tt.as)
		}
		if st != tt.ss {
			t.Fatalf("Failed to find attester slashing with the correct status: %v", tt.as)
		}

		err = db.SaveAttesterSlashing(SlashingStatus(Included), tt.as)
		has, st, err = db.HasAttesterSlashing(tt.as)
		if err != nil {
			t.Fatalf("Failed to get attester slashing: %v", err)
		}
		if !has {
			t.Fatalf("Failed to find attester slashing: %v", tt.as)
		}
		if st != Included {
			t.Fatalf("Failed to find attester slashing with the correct status: %v", tt.as)
		}

	}

}

func TestStore_LatestEpochDetected(t *testing.T) {
	app := cli.NewApp()
	set := flag.NewFlagSet("test", 0)
	ctx := cli.NewContext(app, set, nil)
	db := SetupSlasherDB(t, ctx)
	defer TeardownSlasherDB(t, db)
	e, err := db.GetLatestEpochDetected()
	if err != nil {
		t.Fatalf("Get latest epoch detected failed: %v", err)
	}
	if e != 0 {
		t.Fatalf("Latest epoch detected should have been 0 before setting got: %d", e)
	}
	epoch := uint64(1)
	err = db.SetLatestEpochDetected(epoch)
	if err != nil {
		t.Fatalf("Set latest epoch detected failed: %v", err)
	}
	e, err = db.GetLatestEpochDetected()
	if err != nil {
		t.Fatalf("Get latest epoch detected failed: %v", err)
	}
	if e != epoch {
		t.Fatalf("Latest epoch detected should have been: %d got: %d", epoch, e)
	}
}

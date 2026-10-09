// SPDX-License-Identifier: Apache-2.0

package node

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	sdkmath "cosmossdk.io/math"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	v1base "github.com/sentinel-official/sentinelhub/v12/types/v1"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/trinitystake/dvpnd/v9/context"
	"github.com/trinitystake/dvpnd/v9/types"
)

type fakeSession struct {
	status         v1base.Status
	upload, downld int64
}

func (f fakeSession) GetStatus() v1base.Status      { return f.status }
func (f fakeSession) GetUploadBytes() sdkmath.Int   { return sdkmath.NewInt(f.upload) }
func (f fakeSession) GetDownloadBytes() sdkmath.Int { return sdkmath.NewInt(f.downld) }

// Rules: [SL-14].
func TestSessionNeedsReport(t *testing.T) {
	cases := []struct {
		name  string
		local types.Session
		chain fakeSession
		want  bool
	}{
		{"active and moved data", types.Session{Upload: 10, Download: 20},
			fakeSession{v1base.StatusActive, 0, 0}, true},
		{"active but already reported", types.Session{Upload: 10, Download: 20},
			fakeSession{v1base.StatusActive, 10, 20}, false},
		{"active, only download differs", types.Session{Upload: 10, Download: 25},
			fakeSession{v1base.StatusActive, 10, 20}, true},
		{"inactive is never reported", types.Session{Upload: 10, Download: 20},
			fakeSession{v1base.StatusInactive, 0, 0}, false},
	}
	for _, c := range cases {
		if got := sessionNeedsReport(c.local, c.chain); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// Rules: [SL-14].
func TestClearSessions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&types.Session{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&types.Session{ID: 1, Key: "a", Address: "addr1"})
	db.Create(&types.Session{ID: 2, Key: "b", Address: "addr2", Upload: 5, Download: 7})

	n := NewNode(context.NewContext().WithDatabase(db).WithLogger(cmtlog.NewNopLogger()))

	if err := n.clearSessions(); err != nil {
		t.Fatal(err)
	}

	var count int64
	n.Database().Model(&types.Session{}).Count(&count)
	if count != 0 {
		t.Fatalf("sessions remain after clear: %d", count)
	}
	// A hard (unscoped) delete must leave nothing even for soft-delete queries.
	n.Database().Unscoped().Model(&types.Session{}).Count(&count)
	if count != 0 {
		t.Fatalf("soft-deleted rows remain: %d", count)
	}
}

// TestDeletedSessionsLeaveNoTrace checks the database file itself, not only
// the table: a cleared session's wallet address and peer key must be gone
// from the bytes on disk. A file opened with secure_delete is clean at once;
// one written without it (by an earlier release) still holds them until
// compactDatabase runs. The node's file is opened with OpenDatabase, the one
// door start uses too.
//
// Rules: [PV-4].
func TestDeletedSessionsLeaveNoTrace(t *testing.T) {
	const address, key = "sent1tracemarkeraddress", "tracemarkerpeerkey"

	// earlierRelease opens the file as releases before secure_delete did.
	earlierRelease := func(path string) (*gorm.DB, error) {
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: gormlogger.Discard})
		if err != nil {
			return nil, err
		}
		return db, db.AutoMigrate(&types.Session{})
	}
	for _, tc := range []struct {
		name      string
		open      func(path string) (*gorm.DB, error)
		compact   bool
		wantTrace bool
	}{
		{name: "opened as the node opens it", open: OpenDatabase},
		{name: "earlier release, compacted", open: earlierRelease, compact: true},
		{name: "earlier release, not compacted", open: earlierRelease, wantTrace: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "data.db")
			db, err := tc.open(path)
			if err != nil {
				t.Fatal(err)
			}
			db.Create(&types.Session{ID: 1, Key: key, Address: address})

			n := NewNode(context.NewContext().WithDatabase(db).WithLogger(cmtlog.NewNopLogger()))
			if err := n.clearSessions(); err != nil {
				t.Fatal(err)
			}
			if tc.compact {
				n.compactDatabase()
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			if err := sqlDB.Close(); err != nil {
				t.Fatal(err)
			}

			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			trace := bytes.Contains(raw, []byte(address)) || bytes.Contains(raw, []byte(key))
			if trace != tc.wantTrace {
				t.Fatalf("deleted session left in the file: %v, want %v", trace, tc.wantTrace)
			}
		})
	}
}

// SPDX-License-Identifier: Apache-2.0

package node

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/trinitystake/dvpnd/v9/types"
)

// A client that reconnects after a node restart keeps its on-chain session;
// the service's counters start again from zero, so the totals must be built
// on what the chain already held or the chain refuses the report.
//
// Rules: [SL-2].
func TestReportedUsage(t *testing.T) {
	cases := []struct {
		name             string
		item             types.Session
		peer             types.Peer
		wantUp, wantDown int64
		wantMoved        bool
	}{
		{"fresh session, nothing moved",
			types.Session{}, types.Peer{}, 0, 0, false},
		{"fresh session, data moved",
			types.Session{}, types.Peer{Upload: 10, Download: 20}, 10, 20, true},
		{"re-admitted session, nothing moved yet",
			types.Session{Upload: 300, Download: 400, BaseUpload: 300, BaseDownload: 400},
			types.Peer{}, 300, 400, false},
		{"re-admitted session, data moved: totals stay above the chain's",
			types.Session{Upload: 300, Download: 400, BaseUpload: 300, BaseDownload: 400},
			types.Peer{Upload: 10, Download: 20}, 310, 420, true},
		{"already stored, counters unchanged",
			types.Session{Upload: 310, Download: 420, BaseUpload: 300, BaseDownload: 400},
			types.Peer{Upload: 10, Download: 20}, 310, 420, false},
	}
	for _, c := range cases {
		up, down, moved := reportedUsage(c.item, c.peer)
		if up != c.wantUp || down != c.wantDown || moved != c.wantMoved {
			t.Errorf("%s: got (%d, %d, %v), want (%d, %d, %v)",
				c.name, up, down, moved, c.wantUp, c.wantDown, c.wantMoved)
		}
	}
}

// The counter update names its columns so a zero download is written too
// (a struct update would skip it as a zero value).
//
// Rules: [SL-15].
func TestUsageUpdateWritesBothColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&types.Session{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&types.Session{ID: 1, Key: "a", Address: "addr1", Upload: 5, Download: 7})

	db.Model(&types.Session{}).Where(&types.Session{ID: 1}).Updates(
		map[string]interface{}{"upload": int64(9), "download": int64(0)},
	)

	var item types.Session
	db.Model(&types.Session{}).Where(&types.Session{ID: 1}).First(&item)
	if item.Upload != 9 || item.Download != 0 {
		t.Fatalf("got upload=%d download=%d, want 9 and 0", item.Upload, item.Download)
	}
}

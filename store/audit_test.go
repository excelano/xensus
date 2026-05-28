package store

import (
	"context"
	"database/sql"
	"testing"
)

// insertAuditForTest writes one audit_log row directly, with an explicit
// occurred_at so date-range filtering is deterministic. entityID 0 stores
// NULL, matching how core.WriteAudit treats a zero EntityID.
func insertAuditForTest(t *testing.T, db *sql.DB, occurredAt, actorOID, actorUPN, action, entityType string, entityID int64, details string) {
	t.Helper()
	var entity any
	if entityID > 0 {
		entity = entityID
	}
	var det any
	if details != "" {
		det = details
	}
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO audit_log
			(occurred_at, actor_oid, actor_upn, action, entity_type, entity_id, details)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, occurredAt, actorOID, actorUPN, action, entityType, entity, det)
	if err != nil {
		t.Fatalf("insert audit row: %v", err)
	}
}

// seedAuditLog lays down a small fixed timeline across two actors, three
// entity types, and three calendar days.
func seedAuditLog(t *testing.T, db *sql.DB) {
	t.Helper()
	insertAuditForTest(t, db, "2026-05-26T09:00:00.000Z", "oid-alice", "alice@x.test", "person.create", "person", 1, `{"name":"Jane"}`)
	insertAuditForTest(t, db, "2026-05-27T10:00:00.000Z", "oid-bob", "bob@x.test", "system.create", "system", 1, `{"name":"Workday"}`)
	insertAuditForTest(t, db, "2026-05-27T11:30:00.000Z", "oid-alice", "alice@x.test", "person.rename", "person", 1, `{"from":"Jane","to":"Jane Doe"}`)
	insertAuditForTest(t, db, "2026-05-28T08:15:00.000Z", "oid-bob", "bob@x.test", "tenant.bind", "tenant", 0, "")
	insertAuditForTest(t, db, "2026-05-28T14:45:00.000Z", "oid-alice", "alice@x.test", "person.create", "person", 2, `{"name":""}`)
}

func TestListAudit_NoFilterNewestFirst(t *testing.T) {
	db := openTestDB(t)
	seedAuditLog(t, db)

	events, err := ListAudit(context.Background(), db, AuditQuery{})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("got %d events, want 5", len(events))
	}
	if events[0].Action != "person.create" || events[0].OccurredAt != "2026-05-28T14:45:00.000Z" {
		t.Errorf("newest event wrong: %+v", events[0])
	}
	if events[4].OccurredAt != "2026-05-26T09:00:00.000Z" {
		t.Errorf("oldest event wrong: %+v", events[4])
	}
}

func TestListAudit_FilterEntityType(t *testing.T) {
	db := openTestDB(t)
	seedAuditLog(t, db)

	events, err := ListAudit(context.Background(), db, AuditQuery{EntityType: "person"})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d person events, want 3", len(events))
	}
	for _, e := range events {
		if e.EntityType != "person" {
			t.Errorf("unexpected entity type %q", e.EntityType)
		}
	}
}

func TestListAudit_FilterEntityTypeAndID(t *testing.T) {
	db := openTestDB(t)
	seedAuditLog(t, db)

	events, err := ListAudit(context.Background(), db, AuditQuery{EntityType: "person", EntityID: 1})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events for person 1, want 2", len(events))
	}
	for _, e := range events {
		if e.EntityID != 1 {
			t.Errorf("unexpected entity id %d", e.EntityID)
		}
	}
}

func TestListAudit_FilterActor(t *testing.T) {
	db := openTestDB(t)
	seedAuditLog(t, db)

	events, err := ListAudit(context.Background(), db, AuditQuery{Actor: "alice@x.test"})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events for alice, want 3", len(events))
	}
	for _, e := range events {
		if e.ActorUPN != "alice@x.test" {
			t.Errorf("unexpected actor %q", e.ActorUPN)
		}
	}
}

func TestListAudit_FilterDateRangeInclusive(t *testing.T) {
	db := openTestDB(t)
	seedAuditLog(t, db)

	// A single-day window must include every timestamp on that day, both
	// the early-morning and late-afternoon 2026-05-28 events.
	events, err := ListAudit(context.Background(), db, AuditQuery{From: "2026-05-28", To: "2026-05-28"})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events on 2026-05-28, want 2", len(events))
	}

	// A multi-day window with an open lower bound.
	events, err = ListAudit(context.Background(), db, AuditQuery{To: "2026-05-27"})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events through 2026-05-27, want 3", len(events))
	}
}

func TestListAudit_NullEntityIDIsZero(t *testing.T) {
	db := openTestDB(t)
	seedAuditLog(t, db)

	events, err := ListAudit(context.Background(), db, AuditQuery{EntityType: "tenant"})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d tenant events, want 1", len(events))
	}
	if events[0].EntityID != 0 {
		t.Errorf("NULL entity_id should scan to 0, got %d", events[0].EntityID)
	}
	if events[0].Details != "" {
		t.Errorf("NULL details should scan to empty, got %q", events[0].Details)
	}
}

func TestListAudit_LimitCaps(t *testing.T) {
	db := openTestDB(t)
	seedAuditLog(t, db)

	events, err := ListAudit(context.Background(), db, AuditQuery{Limit: 2})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events with limit 2, want 2", len(events))
	}
	// Newest-first means the limit keeps the two most recent.
	if events[0].OccurredAt != "2026-05-28T14:45:00.000Z" {
		t.Errorf("limited result not newest-first: %+v", events[0])
	}
}

func TestListAuditActors_DistinctSorted(t *testing.T) {
	db := openTestDB(t)
	seedAuditLog(t, db)

	actors, err := ListAuditActors(context.Background(), db)
	if err != nil {
		t.Fatalf("ListAuditActors: %v", err)
	}
	want := []string{"alice@x.test", "bob@x.test"}
	if len(actors) != len(want) {
		t.Fatalf("got %d actors, want %d: %v", len(actors), len(want), actors)
	}
	for i := range want {
		if actors[i] != want[i] {
			t.Errorf("actor[%d] = %q, want %q", i, actors[i], want[i])
		}
	}
}

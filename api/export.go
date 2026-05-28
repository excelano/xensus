package api

import (
	"archive/zip"
	"encoding/csv"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/excelano/xensus/id"
	"github.com/excelano/xensus/store"
)

// ExportRegistry handles GET /api/v1/export. Steward-only: a whole-registry
// dump, audit log included, is a heavier action than browsing one surface, so
// it's gated to stewards regardless of the per-surface read policy. It streams
// a .zip with one CSV per entity — persons, systems (active and disabled),
// associations, stewards, and the full audit log.
func (h *Handlers) ExportRegistry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	persons, err := store.ListPersons(ctx, h.DB, "")
	if err != nil {
		httpError(ctx, w, err)
		return
	}
	active, err := store.ListSystems(ctx, h.DB, "")
	if err != nil {
		httpError(ctx, w, err)
		return
	}
	disabled, err := store.ListDisabledSystems(ctx, h.DB)
	if err != nil {
		httpError(ctx, w, err)
		return
	}
	assocs, err := store.ListAllAssociations(ctx, h.DB)
	if err != nil {
		httpError(ctx, w, err)
		return
	}
	stewards, err := store.ListActiveStewards(ctx, h.DB)
	if err != nil {
		httpError(ctx, w, err)
		return
	}
	events, err := store.ListAudit(ctx, h.DB, store.AuditQuery{Limit: store.MaxAuditLimit})
	if err != nil {
		httpError(ctx, w, err)
		return
	}

	// Every read above ran before any header was written, so a DB failure
	// still produces a clean error status. Once the zip starts streaming the
	// 200 is committed, so a write failure from there on can only be logged.
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="xensus-export.zip"`)

	zw := zip.NewWriter(w)
	defer zw.Close()

	entries := []struct {
		name  string
		write func(*csv.Writer)
	}{
		{"persons.csv", func(cw *csv.Writer) { writePersonsCSV(cw, persons) }},
		{"systems.csv", func(cw *csv.Writer) { writeSystemsExportCSV(cw, active, disabled) }},
		{"associations.csv", func(cw *csv.Writer) { writeAssociationsCSV(cw, assocs) }},
		{"stewards.csv", func(cw *csv.Writer) { writeStewardsCSV(cw, stewards) }},
		{"audit.csv", func(cw *csv.Writer) { writeAuditCSV(cw, events) }},
	}
	for _, e := range entries {
		fw, err := zw.Create(e.name)
		if err != nil {
			slog.ErrorContext(ctx, "export: create zip entry", "file", e.name, "err", err)
			return
		}
		cw := csv.NewWriter(fw)
		e.write(cw)
		cw.Flush()
		if err := cw.Error(); err != nil {
			slog.ErrorContext(ctx, "export: write csv", "file", e.name, "err", err)
			return
		}
	}
}

// The writers below each emit a header row plus data rows and leave flushing
// to the caller. The per-entity export endpoints and the full-registry zip
// share them so a given entity's CSV columns are identical wherever it appears.

func writePersonsCSV(cw *csv.Writer, persons []store.Person) {
	_ = cw.Write([]string{"id", "name", "created_at", "created_by", "updated_at", "updated_by"})
	for _, p := range persons {
		_ = cw.Write([]string{id.Format(p.ID), p.Name, p.CreatedAt, p.CreatedBy, p.UpdatedAt, p.UpdatedBy})
	}
}

// writeSystemsExportCSV writes active systems followed by disabled ones, the
// disabled_* columns empty for active rows. This is the complete-snapshot
// shape; the /systems.csv endpoint stays active-only (its working set).
func writeSystemsExportCSV(cw *csv.Writer, active, disabled []store.System) {
	_ = cw.Write([]string{"id", "name", "created_at", "created_by", "updated_at", "updated_by", "disabled_at", "disabled_by"})
	write := func(s store.System) {
		_ = cw.Write([]string{
			strconv.FormatInt(s.ID, 10), s.Name, s.CreatedAt, s.CreatedBy,
			s.UpdatedAt, s.UpdatedBy, s.DisabledAt, s.DisabledBy,
		})
	}
	for _, s := range active {
		write(s)
	}
	for _, s := range disabled {
		write(s)
	}
}

func writeAssociationsCSV(cw *csv.Writer, assocs []store.Association) {
	_ = cw.Write([]string{"id", "person_id", "system_id", "system", "foreign_id", "created_at", "created_by"})
	for _, a := range assocs {
		_ = cw.Write([]string{
			strconv.FormatInt(a.ID, 10), id.Format(a.PersonID), strconv.FormatInt(a.SystemID, 10),
			a.SystemName, a.ForeignID, a.CreatedAt, a.CreatedBy,
		})
	}
}

func writeStewardsCSV(cw *csv.Writer, stewards []store.Steward) {
	_ = cw.Write([]string{"id", "oid", "upn", "promoted_at", "promoted_by"})
	for _, s := range stewards {
		_ = cw.Write([]string{strconv.FormatInt(s.ID, 10), s.UserOID, s.UserUPN, s.PromotedAt, s.PromotedBy})
	}
}

func writeAuditCSV(cw *csv.Writer, events []store.AuditEvent) {
	_ = cw.Write([]string{"id", "occurred_at", "actor_oid", "actor_upn", "action", "entity_type", "entity_id", "details"})
	for _, e := range events {
		entityID := ""
		if e.EntityID > 0 {
			entityID = strconv.FormatInt(e.EntityID, 10)
		}
		_ = cw.Write([]string{
			strconv.FormatInt(e.ID, 10), e.OccurredAt, e.ActorOID, e.ActorUPN,
			e.Action, e.EntityType, entityID, e.Details,
		})
	}
}

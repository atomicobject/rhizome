// Package admin serves the admin API routes in wire. Every route needs a
// principal with Admin set, checked before any lookup, and every change is
// appended to the audit log.
package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/auth"
	"github.com/atomicobject/rhizome/services/team-api/internal/config"
	"github.com/atomicobject/rhizome/services/team-api/internal/core"
	"github.com/atomicobject/rhizome/services/team-api/internal/store"
)

const subsystem = "teamapi.admin"

type Handler struct {
	cfg     config.Config
	store   store.Store
	runtime *auth.Runtime
	usage   core.UsageReader
	rec     *diagnostics.Recorder
	now     func() time.Time
}

// New builds the handler. usage and rec may be nil.
func New(cfg config.Config, st store.Store, rt *auth.Runtime, usage core.UsageReader, rec *diagnostics.Recorder, now func() time.Time) *Handler {
	return &Handler{cfg: cfg, store: st, runtime: rt, usage: usage, rec: rec, now: now}
}

// Routes registers every admin route on mux, each wrapped by authn, which
// must put a core.Principal in the context.
func (h *Handler) Routes(mux *http.ServeMux, authn func(http.Handler) http.Handler) {
	routes := map[string]func(http.ResponseWriter, *http.Request, core.Principal){
		"GET " + wire.RouteAdminUsage:      h.getUsage,
		"GET " + wire.RouteAdminPeople:     h.getPeople,
		"PATCH " + wire.RouteAdminPerson:   h.patchPerson,
		"GET " + wire.RouteAdminSessions:   h.getSessions,
		"POST " + wire.RouteAdminRevoke:    h.revokeSession,
		"POST " + wire.RouteAdminRevokeAll: h.revokePerson,
		"GET " + wire.RouteAdminConfig:     h.getConfig,
		"PATCH " + wire.RouteAdminConfig:   h.patchConfig,
		"GET " + wire.RouteAdminAudit:      h.getAudit,
	}
	for pattern, fn := range routes {
		mux.Handle(pattern, authn(requireAdmin(fn)))
	}
}

func requireAdmin(fn func(http.ResponseWriter, *http.Request, core.Principal)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := core.PrincipalFrom(r.Context())
		if !ok || !p.Admin {
			auth.WriteError(w, wire.CodeNotAdmin, "this account is not a team API admin")
			return
		}
		fn(w, r, p)
	})
}

func internal(w http.ResponseWriter) {
	auth.WriteError(w, wire.CodeInternal, "the gateway store is unavailable")
}

func (h *Handler) getUsage(w http.ResponseWriter, r *http.Request, _ core.Principal) {
	date := r.URL.Query().Get("date")
	if date == "" {
		date = h.now().UTC().Format(time.DateOnly)
	} else if _, err := time.Parse(time.DateOnly, date); err != nil {
		auth.WriteError(w, wire.CodeBadRequest, "date must be YYYY-MM-DD")
		return
	}
	rt, err := h.runtime.Runtime(r.Context())
	if err != nil {
		internal(w)
		return
	}
	people, err := h.store.People(r.Context())
	if err != nil {
		internal(w)
		return
	}
	var global wire.DailyUsage
	var byPerson map[string]wire.DailyUsage
	if h.usage != nil {
		if global, byPerson, err = h.usage.Usage(r.Context(), date); err != nil {
			internal(w)
			return
		}
	}
	out := wire.AdminUsage{Date: date, Global: wire.UsageWithLimits{Usage: global, Limits: rt.GlobalLimits}, People: []wire.PersonUsage{}}
	for _, p := range people {
		limits := rt.PersonLimits
		if p.Limits != nil {
			limits = *p.Limits
		}
		out.People = append(out.People, wire.PersonUsage{Sub: p.Sub, Email: p.Email, UsageWithLimits: wire.UsageWithLimits{Usage: byPerson[p.Sub], Limits: limits}})
	}
	auth.WriteJSON(w, out)
}

func (h *Handler) adminPerson(p store.Person) wire.AdminPerson {
	return wire.AdminPerson{
		Sub: p.Sub, Email: p.Email, HostedDomain: p.HostedDomain, Status: p.Status, CreatedAt: p.CreatedAt,
		Admin: p.Status == wire.PersonActive && slices.Contains(h.cfg.Admins, p.Email), LimitsOverride: p.Limits,
	}
}

func (h *Handler) getPeople(w http.ResponseWriter, r *http.Request, _ core.Principal) {
	people, err := h.store.People(r.Context())
	if err != nil {
		internal(w)
		return
	}
	out := []wire.AdminPerson{}
	for _, p := range people {
		out = append(out, h.adminPerson(p))
	}
	auth.WriteJSON(w, out)
}

func (h *Handler) patchPerson(w http.ResponseWriter, r *http.Request, actor core.Principal) {
	start := h.now()
	var u wire.AdminPersonUpdate
	if !auth.DecodeJSON(w, r, &u) {
		return
	}
	if u.Status != nil && *u.Status != wire.PersonActive && *u.Status != wire.PersonDisabled {
		auth.WriteError(w, wire.CodeBadRequest, "status must be active or disabled")
		return
	}
	if u.Limits != nil && !config.Within(*u.Limits, *h.cfg.MaxPersonLimits) {
		auth.WriteError(w, wire.CodeBadRequest, "limits must be between zero and the server's max_person_limits")
		return
	}
	if u.Limits != nil && u.ClearLimits {
		auth.WriteError(w, wire.CodeBadRequest, "set limits or clear_limits, not both")
		return
	}
	sub := r.PathValue("sub")
	var detail []string
	if u.Status != nil {
		detail = append(detail, "status="+*u.Status)
	}
	if u.Limits != nil {
		detail = append(detail, "limits="+limitsText(*u.Limits))
	}
	if u.ClearLimits {
		detail = append(detail, "limits=default")
	}
	p, err := h.store.UpdatePerson(r.Context(), sub, u, h.entry(actor, "person.update", sub, strings.Join(detail, " ")))
	if h.done(r.Context(), w, actor, "person.update", sub, err, start, "no such person") {
		auth.WriteJSON(w, h.adminPerson(p))
	}
}

func (h *Handler) getSessions(w http.ResponseWriter, r *http.Request, _ core.Principal) {
	sessions, err := h.store.Sessions(r.Context(), r.URL.Query().Get("sub"))
	if err != nil {
		internal(w)
		return
	}
	out := []wire.AdminSession{}
	for _, x := range sessions {
		s := wire.AdminSession{
			SessionID: x.ID, Sub: x.Sub, DeviceName: x.DeviceName, ClientVersion: x.ClientVersion,
			CreatedAt: x.CreatedAt, LastUsedAt: x.LastUsedAt, SessionExpiresAt: x.AbsoluteExpiresAt,
		}
		if !x.RevokedAt.IsZero() {
			at := x.RevokedAt
			s.RevokedAt = &at
		}
		out = append(out, s)
	}
	auth.WriteJSON(w, out)
}

func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request, actor core.Principal) {
	start := h.now()
	id := r.PathValue("id")
	e := h.entry(actor, "session.revoke", id, "")
	err := h.store.RevokeSession(r.Context(), id, "admin", start, &e)
	if h.done(r.Context(), w, actor, "session.revoke", id, err, start, "no such session") {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) revokePerson(w http.ResponseWriter, r *http.Request, actor core.Principal) {
	start := h.now()
	sub := r.PathValue("sub")
	_, err := h.store.RevokePersonSessions(r.Context(), sub, "admin", start, h.entry(actor, "person.revoke_sessions", sub, ""))
	if h.done(r.Context(), w, actor, "person.revoke_sessions", sub, err, start, "no such person") {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) getConfig(w http.ResponseWriter, r *http.Request, _ core.Principal) {
	h.runtime.Invalidate()
	rt, err := h.runtime.Runtime(r.Context())
	if err != nil {
		internal(w)
		return
	}
	auth.WriteJSON(w, rt)
}

func (h *Handler) patchConfig(w http.ResponseWriter, r *http.Request, actor core.Principal) {
	start := h.now()
	var u wire.AdminConfigUpdate
	if !auth.DecodeJSON(w, r, &u) {
		return
	}
	if m := u.EligibilityMode; m != nil && *m != wire.EligibilityAll && *m != wire.EligibilityAllowlist {
		auth.WriteError(w, wire.CodeBadRequest, "eligibility_mode must be all or allowlist")
		return
	}
	if (u.PersonLimits != nil && !config.Within(*u.PersonLimits, *h.cfg.MaxPersonLimits)) ||
		(u.GlobalLimits != nil && !config.Within(*u.GlobalLimits, *h.cfg.MaxGlobalLimits)) {
		auth.WriteError(w, wire.CodeBadRequest, "limits must be between zero and the server's max_person_limits and max_global_limits")
		return
	}
	if u.Allowlist != nil {
		list := []string{}
		for _, e := range *u.Allowlist {
			if e = strings.ToLower(strings.TrimSpace(e)); e != "" && !slices.Contains(list, e) {
				list = append(list, e)
			}
		}
		u.Allowlist = &list
	}
	_, err := h.store.UpdateRuntimeConfig(r.Context(), u, h.entry(actor, "config.update", "config", configDetail(u)))
	h.runtime.Invalidate()
	if !h.done(r.Context(), w, actor, "config.update", "config", err, start, "") {
		return
	}
	rt, err := h.runtime.Runtime(r.Context())
	if err != nil {
		internal(w)
		return
	}
	auth.WriteJSON(w, rt)
}

func (h *Handler) getAudit(w http.ResponseWriter, r *http.Request, _ core.Principal) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			auth.WriteError(w, wire.CodeBadRequest, "limit must be a positive integer")
			return
		}
		limit = min(n, 1000)
	}
	entries, err := h.store.Audit(r.Context(), limit)
	if err != nil {
		internal(w)
		return
	}
	auth.WriteJSON(w, entries)
}

func (h *Handler) entry(actor core.Principal, action, target, detail string) wire.AuditEntry {
	return wire.AuditEntry{At: h.now(), Actor: actor.Email, Action: action, Target: target, Detail: detail}
}

// done records the diagnostics event for a mutation that committed with its
// audit entry, or writes the error response and returns false.
func (h *Handler) done(ctx context.Context, w http.ResponseWriter, actor core.Principal, action, target string, err error, start time.Time, notFound string) bool {
	outcome := "ok"
	switch {
	case errors.Is(err, store.ErrNotFound) && notFound != "":
		outcome = string(wire.CodeNotFound)
		auth.WriteError(w, wire.CodeNotFound, notFound)
	case err != nil:
		outcome = string(wire.CodeInternal)
		internal(w)
	}
	auth.RecordEvent(ctx, h.rec, subsystem, "admin."+action, outcome, h.now().Sub(start),
		slog.String("sub", actor.Sub), slog.String("session_id", actor.SessionID), slog.String("target", target))
	return err == nil
}

func negative(l wire.DailyLimits) bool {
	return l.Requests < 0 || l.InputBytes < 0 || l.ChargeCredits < 0
}

func limitsText(l wire.DailyLimits) string {
	return fmt.Sprintf("requests:%d,input_bytes:%d,charge_credits:%d", l.Requests, l.InputBytes, l.ChargeCredits)
}

func configDetail(u wire.AdminConfigUpdate) string {
	var parts []string
	for name, v := range map[string]*bool{"paid_calls_enabled": u.PaidCallsEnabled, "voyage_enabled": u.VoyageEnabled, "typesafe_enabled": u.TypeSafeEnabled} {
		if v != nil {
			parts = append(parts, fmt.Sprintf("%s=%t", name, *v))
		}
	}
	if u.EligibilityMode != nil {
		parts = append(parts, "eligibility_mode="+*u.EligibilityMode)
	}
	if u.Allowlist != nil {
		parts = append(parts, fmt.Sprintf("allowlist=%d entries", len(*u.Allowlist)))
	}
	if u.PersonLimits != nil {
		parts = append(parts, "person_limits="+limitsText(*u.PersonLimits))
	}
	if u.GlobalLimits != nil {
		parts = append(parts, "global_limits="+limitsText(*u.GlobalLimits))
	}
	slices.Sort(parts)
	return strings.Join(parts, " ")
}

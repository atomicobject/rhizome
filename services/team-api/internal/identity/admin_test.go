package identity_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

var adminRoutes = []struct{ method, path string }{
	{"GET", wire.RouteAdminUsage},
	{"GET", wire.RouteAdminPeople},
	{"PATCH", "/v1/admin/people/" + adminSub},
	{"GET", wire.RouteAdminSessions},
	{"POST", "/v1/admin/sessions/no-such-session/revoke"},
	{"POST", "/v1/admin/people/" + adminSub + "/revoke"},
	{"GET", wire.RouteAdminConfig},
	{"PATCH", wire.RouteAdminConfig},
	{"GET", wire.RouteAdminAudit},
}

func TestNonAdminGetsNotAdminEverywhere(t *testing.T) {
	e := newEnv(t, options{})
	e.signIn(adminSub, adminEmail)
	g := e.signIn(memberSub, member)
	for _, rt := range adminRoutes {
		var body any
		if rt.method == "PATCH" {
			body = map[string]any{}
		}
		if r := e.do(rt.method, rt.path, g.AccessToken, body); r.code != wire.CodeNotAdmin || r.status != http.StatusForbidden {
			t.Errorf("%s %s: got %d %q", rt.method, rt.path, r.status, r.code)
		}
		if r := e.do(rt.method, rt.path, "", body); r.code != wire.CodeSessionRevoked {
			t.Errorf("%s %s without a token: got %d %q", rt.method, rt.path, r.status, r.code)
		}
	}
	// The admin passes the same check.
	admin := e.signIn(adminSub, adminEmail)
	e.decode(e.do("GET", wire.RouteAdminPeople, admin.AccessToken, nil), &[]wire.AdminPerson{})
}

func TestAdminChangesAreAudited(t *testing.T) {
	e := newEnv(t, options{usage: usageReader{"GLOBAL": {Requests: 9}, memberSub: {Requests: 4}}})
	admin := e.signIn(adminSub, adminEmail)
	g := e.signIn(memberSub, member)
	tok := admin.AccessToken

	var usage wire.AdminUsage
	e.decode(e.do("GET", wire.RouteAdminUsage+"?date=2026-10-30", tok, nil), &usage)
	if usage.Date != "2026-10-30" || usage.Global.Usage.Requests != 9 || usage.Global.Limits.Requests != 1000 || len(usage.People) != 2 {
		t.Fatalf("usage = %+v", usage)
	}
	e.wantCode(e.do("GET", wire.RouteAdminUsage+"?date=yesterday", tok, nil), wire.CodeBadRequest)

	var people []wire.AdminPerson
	e.decode(e.do("GET", wire.RouteAdminPeople, tok, nil), &people)
	if len(people) != 2 || people[0].Email != adminEmail || !people[0].Admin || people[1].Admin {
		t.Fatalf("people = %+v", people)
	}

	limits := wire.DailyLimits{Requests: 7, InputBytes: 8, ChargeCredits: 9}
	var person wire.AdminPerson
	e.decode(e.do("PATCH", "/v1/admin/people/"+memberSub, tok, wire.AdminPersonUpdate{Limits: &limits}), &person)
	if person.Status != wire.PersonActive || person.LimitsOverride == nil || *person.LimitsOverride != limits {
		t.Fatalf("person = %+v", person)
	}
	bad := wire.DailyLimits{Requests: -1}
	e.wantCode(e.do("PATCH", "/v1/admin/people/"+memberSub, tok, wire.AdminPersonUpdate{Limits: &bad}), wire.CodeBadRequest)
	status := "suspended"
	e.wantCode(e.do("PATCH", "/v1/admin/people/"+memberSub, tok, wire.AdminPersonUpdate{Status: &status}), wire.CodeBadRequest)
	e.wantCode(e.do("PATCH", "/v1/admin/people/nobody", tok, wire.AdminPersonUpdate{}), wire.CodeNotFound)
	// Ceilings default to ten times the configured defaults.
	over := wire.DailyLimits{Requests: 1001}
	e.wantCode(e.do("PATCH", "/v1/admin/people/"+memberSub, tok, wire.AdminPersonUpdate{Limits: &over}), wire.CodeBadRequest)
	e.wantCode(e.do("PATCH", wire.RouteAdminConfig, tok, wire.AdminConfigUpdate{PersonLimits: &over}), wire.CodeBadRequest)
	overGlobal := wire.DailyLimits{Requests: 10, InputBytes: 100001}
	e.wantCode(e.do("PATCH", wire.RouteAdminConfig, tok, wire.AdminConfigUpdate{GlobalLimits: &overGlobal}), wire.CodeBadRequest)

	var sessions []wire.AdminSession
	e.decode(e.do("GET", wire.RouteAdminSessions+"?sub="+memberSub, tok, nil), &sessions)
	if len(sessions) != 1 || sessions[0].SessionID != g.SessionID || sessions[0].ClientVersion != "1.0.0" {
		t.Fatalf("sessions = %+v", sessions)
	}
	if r := e.do("POST", "/v1/admin/sessions/"+g.SessionID+"/revoke", tok, nil); r.status != http.StatusNoContent {
		t.Fatalf("revoke = %d %s", r.status, r.body)
	}
	e.wantCode(e.do("POST", "/v1/admin/sessions/missing/revoke", tok, nil), wire.CodeNotFound)
	if r := e.do("POST", "/v1/admin/people/"+memberSub+"/revoke", tok, nil); r.status != http.StatusNoContent {
		t.Fatalf("revoke all = %d %s", r.status, r.body)
	}

	off := false
	var cfg wire.AdminConfig
	e.decode(e.do("PATCH", wire.RouteAdminConfig, tok, wire.AdminConfigUpdate{PaidCallsEnabled: &off}), &cfg)
	if cfg.PaidCallsEnabled || !cfg.VoyageEnabled || cfg.EligibilityMode != wire.EligibilityAll ||
		cfg.PersonLimits.Requests != 100 || len(cfg.AcceptedDomains) != 1 || cfg.Admins[0] != adminEmail {
		t.Fatalf("config after partial update = %+v", cfg)
	}
	mode := "everyone"
	e.wantCode(e.do("PATCH", wire.RouteAdminConfig, tok, wire.AdminConfigUpdate{EligibilityMode: &mode}), wire.CodeBadRequest)
	e.wantCode(e.do("PATCH", wire.RouteAdminConfig, tok, []byte(`{"admins":["mallory@atomicobject.com"]}`)), wire.CodeBadRequest)
	e.decode(e.do("GET", wire.RouteAdminConfig, tok, nil), &cfg)
	if cfg.PaidCallsEnabled || len(cfg.Admins) != 1 {
		t.Fatalf("config = %+v", cfg)
	}
	rt, err := e.id.Runtime().Runtime(t.Context())
	if err != nil || rt.PaidCallsEnabled {
		t.Fatalf("runtime source = %+v, %v", rt, err)
	}

	var audit []wire.AuditEntry
	e.decode(e.do("GET", wire.RouteAdminAudit+"?limit=10", tok, nil), &audit)
	want := []struct{ action, target string }{
		{"config.update", "config"},
		{"person.revoke_sessions", memberSub},
		{"session.revoke", g.SessionID},
		{"person.update", memberSub},
	}
	if len(audit) != len(want) {
		t.Fatalf("audit = %+v", audit)
	}
	for i, w := range want {
		a := audit[i]
		if a.Action != w.action || a.Target != w.target || a.Actor != adminEmail || a.At.IsZero() {
			t.Errorf("audit[%d] = %+v, want %s on %s", i, a, w.action, w.target)
		}
		if strings.Contains(a.Detail, "rzt_") {
			t.Errorf("audit detail contains a token: %q", a.Detail)
		}
	}
	if audit[0].Detail != "paid_calls_enabled=false" || audit[3].Detail != "limits=requests:7,input_bytes:8,charge_credits:9" {
		t.Errorf("audit details = %q, %q", audit[0].Detail, audit[3].Detail)
	}
}

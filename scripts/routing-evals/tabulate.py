import json, glob, os, sys
OUT = os.environ.get("RZM_EVAL_OUT", "/tmp/routing-evals")
scen = {s["id"]: s for s in json.load(open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "scenarios.json")))}
dec = json.JSONDecoder()
def parse(raw):
    i = raw.find("{"); t = raw[i:].strip()
    for cand in (t, t + "}", t.rstrip(",") + "}"):
        try: return dec.raw_decode(cand)[0]
        except Exception: pass
    return {"_parse_error": True}
print("| Scenario | Model | Route | Stops before work | Tests | Escalates | Concern docs |")
print("| --- | --- | --- | --- | --- | --- | --- |")
for p in sorted(glob.glob(os.path.join(OUT, "*.json"))):
    r = json.load(open(p)); v = parse(r.get("raw", "") or "")
    e = scen.get(r["id"], {}).get("expect", {})
    docs = [d.split("/")[-1] for d in (v.get("docs_consulted") or []) if str(d).startswith("docs/engineering")]
    stop = ("yes: " + str(v.get("stop_reason") or "")[:140]) if v.get("stop_before_work") else "no"
    mark = lambda k, ek: "" if v.get(k) == e.get(ek) else " (!)"
    print(f"| {r['id']} | {r['model']} | {str(v.get('route'))[:80]} | {stop}{mark('stop_before_work','stop')} | {'yes' if v.get('would_write_tests') else 'no'}{mark('would_write_tests','tests')} | {'yes' if v.get('escalate_to_effort') else 'no'}{mark('escalate_to_effort','escalate')} | {', '.join(docs)} |")

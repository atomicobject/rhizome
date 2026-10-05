import json, subprocess, sys, os, concurrent.futures, time
REPO=os.environ.get("RZM_EVAL_REPO", os.getcwd())
OUT=os.environ.get("RZM_EVAL_OUT","/tmp/routing-evals"); os.makedirs(OUT, exist_ok=True)
scen=json.load(open(os.path.join(os.path.dirname(os.path.abspath(__file__)),"scenarios.json")))
SUFFIX = """

---
This is a routing evaluation. Do NOT perform the task and do NOT edit anything. Read whatever repository guidance you would normally read first (AGENTS.md, skills, docs/engineering), then answer with ONLY a JSON object (no prose, no code fence) with these keys:
{"route": "<skill and phase you would invoke, or 'none'>", "docs_consulted": ["<repo docs you would read before acting>"], "stop_before_work": true|false, "stop_reason": "<why you would stop for the user, or ''>", "would_write_tests": true|false, "escalate_to_effort": true|false, "notes": "<one sentence>"}"""
def run(model, s):
    prompt = s["prompt"] + SUFFIX
    out_path=os.path.join(OUT, f"{s['id']}.{model}.json")
    if os.path.exists(out_path): return
    t0=time.time()
    try:
        if model=="fable":
            cmd=[os.path.join(REPO,"scripts","claude-fable"), prompt]
            r=subprocess.run(cmd, cwd=REPO, capture_output=True, text=True, timeout=420)
            text=r.stdout
        else:
            cmd=["codex","exec","--skip-git-repo-check","-s","read-only","-C",REPO,"-o",out_path+".raw",prompt]
            r=subprocess.run(cmd, capture_output=True, text=True, timeout=420)
            text=open(out_path+".raw").read() if os.path.exists(out_path+".raw") else r.stdout
        rec={"id":s["id"],"model":model,"seconds":round(time.time()-t0),"raw":text,"stderr":r.stderr[-2000:]}
    except Exception as e:
        rec={"id":s["id"],"model":model,"seconds":round(time.time()-t0),"error":str(e)}
    json.dump(rec, open(out_path,"w"), indent=1)
    print(model, s["id"], rec.get("seconds"), "error" if "error" in rec else "ok", flush=True)
jobs=[(m,s) for s in scen for m in ("fable","astra")]
with concurrent.futures.ThreadPoolExecutor(max_workers=4) as ex:
    list(ex.map(lambda j: run(*j), jobs))
print("DONE")

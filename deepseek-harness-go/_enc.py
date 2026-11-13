import os, sys, json

ROOT = r"D:\Devops\AgentProgram\deepseek-harness-all\deepseek-harness-go\web"
OUT = open(r"D:\Devops\AgentProgram\deepseek-harness-all\deepseek-harness-go\_enc.txt", "w", encoding="utf-8")


def w(*a):
    OUT.write(" ".join(str(x) for x in a) + "\n")


bad = []
for dirpath, dirnames, filenames in os.walk(ROOT):
    dirnames[:] = [d for d in dirnames if d not in ("node_modules", "dist")]
    for fn in filenames:
        p = os.path.join(dirpath, fn)
        if not fn.endswith((".ts", ".vue", ".json", ".html", ".css")):
            continue
        raw = open(p, "rb").read()
        rel = os.path.relpath(p, ROOT)
        try:
            raw.decode("utf-8")
            dec = True
        except UnicodeDecodeError as e:
            dec = False
            w("NOT-UTF8:", rel, str(e))
            bad.append(rel)
            continue
        # detect mojibake: replacement chars or suspicious latin1->utf8 sequences
        if b"\xef\xbf\xbd" in raw:
            w("REPLACEMENT-CHAR:", rel)
            bad.append(rel)
        # find the description string
        if fn == "package.json":
            j = json.loads(raw.decode("utf-8"))
            w("package.json description bytes ->", repr(j.get("description")))

# scan all files for U+FFFD after decode
for dirpath, dirnames, filenames in os.walk(ROOT):
    dirnames[:] = [d for d in dirnames if d not in ("node_modules", "dist")]
    for fn in filenames:
        if not fn.endswith((".ts", ".vue", ".json", ".html", ".css")):
            continue
        p = os.path.join(dirpath, fn)
        rel = os.path.relpath(p, ROOT)
        txt = open(p, encoding="utf-8", errors="replace").read()
        for i, line in enumerate(txt.splitlines(), 1):
            if "\ufffd" in line:
                w(f"U+FFFD {rel}:{i}: {line.strip()[:160]}")

w("done. bad files:", len(bad))
OUT.close()
print("ok")

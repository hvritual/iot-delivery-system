#!/usr/bin/env python3
"""Fail-closed task, independent-review and pre-merge checks (Python stdlib).

Production review evidence is fetched from GitHub, never from a caller-supplied
JSON approval. This program performs no merge or repository-setting mutation.
"""
from __future__ import annotations
import argparse
import base64
import datetime as dt
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request


class Blocked(RuntimeError):
    pass


def require(value, message):
    if not value:
        raise Blocked(message)


def strict_json(data):
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, f"duplicate JSON key: {key}")
            result[key] = value
        return result
    return json.loads(data, object_pairs_hook=pairs)


def git(root, *args):
    p = subprocess.run(["git", "-C", str(root), *args], capture_output=True, check=False)
    require(p.returncode == 0, "Git evidence unavailable: " + p.stderr.decode(errors="replace"))
    return p.stdout


def sha(value):
    require(isinstance(value, str) and re.fullmatch(r"[0-9a-f]{40}", value), "immutable SHA required")
    return value


def control(path):
    return (path.startswith((".architecture/", ".github/workflows/", "tools/archgate/"))
            or path in {"AGENTS.md", "scripts/delivery_gates.py", "scripts/test_delivery_gates.py",
                        "scripts/check-architecture.sh", "backend-yunka/architecture_contract_test.go",
                        "backend-yunka/no_bypass_guard_test.go", "backend-yunka/yu32_application_boundary_test.go"}
            or path.startswith("backend-yunka/scripts/run-yu"))


def validate_task(plan, base, changed):
    required = {"schema", "id", "kind", "base", "goal", "allowed_files", "invariants", "counterexamples", "next"}
    require(isinstance(plan, dict) and set(plan) == required, "invalid task contract schema")
    require(plan["schema"] == 1 and re.fullmatch(r"ARCH-[0-9]{2}[A-Z]?", plan["id"]), "invalid task identity")
    require(plan["kind"] in {"control", "refactor", "feature", "fix"}, "invalid task kind")
    require(sha(plan["base"]) == sha(base), "task base is stale; rebase and revalidate, do not reuse receipts")
    require(isinstance(plan["goal"], str) and plan["goal"].strip(), "missing change goal")
    for key in ("invariants", "counterexamples"):
        require(isinstance(plan[key], list) and len(plan[key]) >= 1
                and all(isinstance(x, str) and x.strip() for x in plan[key]), f"missing {key}")
    allowed = plan["allowed_files"]
    require(isinstance(allowed, list) and allowed and len(set(allowed)) == len(allowed), "invalid exact path list")
    for name in allowed:
        require(isinstance(name, str) and str(PurePosixPath(name)) == name
                and not name.startswith("/") and ".." not in PurePosixPath(name).parts
                and not any(c in name for c in "*?[]\\\n\r"), "paths must be exact, relative and non-glob")
    require(changed, "empty change cannot be certified")
    require(set(changed) <= set(allowed), "unplanned files: " + ", ".join(sorted(set(changed) - set(allowed))))
    require(not any(p.startswith(("backend/", "third_party/")) or p in {".gitmodules", "third_party/yunka", "backend"} for p in changed),
            "legacy backend and framework source/gitlink are read-only in this workstream")
    controls = [p for p in changed if control(p) and p != ".architecture/change.json"]
    business = [p for p in changed if p.startswith(("backend-yunka/internal/", "backend-yunka/contracts/",
                                                    "backend-yunka/modules/", "backend-yunka/cmd/", "web/"))]
    if controls:
        require(plan["kind"] == "control", "gate changes require a separate control task")
        require(not business, "cannot alter governance and governed business code in one task")
    require(all(not p.endswith(".go") or p.startswith(("backend-yunka/", "tools/archgate/")) for p in changed),
            "new production Go scope outside the certified modules requires a separate gate extension")
    return {"task": plan["id"], "kind": plan["kind"], "base": base,
            "changed_files": sorted(changed), "control_change": bool(controls)}


def check_task(root, base):
    head = sha(git(root, "rev-parse", "HEAD").decode().strip())
    base = sha(base)
    git(root, "merge-base", "--is-ancestor", base, head)
    require(not git(root, "status", "--porcelain", "--untracked-files=all"), "dirty worktree cannot produce a receipt")
    raw = git(root, "show", f"{head}:.architecture/change.json")
    plan = strict_json(raw)
    changed = [p.decode() for p in git(root, "diff", "--name-only", "-z", "--no-renames", base, head).split(b"\0") if p]
    result = validate_task(plan, base, changed)
    current = strict_json(git(root, "show", f"{head}:.architecture/policy.json"))
    # Once installed, ordinary control changes cannot silently reset the debt
    # baseline or framework pin. An explicitly redesigned upgrade is separate.
    probe = subprocess.run(["git", "-C", str(root), "cat-file", "-e", f"{base}:.architecture/policy.json"], capture_output=True)
    if probe.returncode == 0:
        old = strict_json(git(root, "show", f"{base}:.architecture/policy.json"))
        require(current["baseline"] == old["baseline"], "immutable debt baseline was reset")
        require(current["framework"] == old["framework"], "framework pin changed")
    result.update(head=head, task_sha256=hashlib.sha256(raw).hexdigest(),
                  policy_sha256=hashlib.sha256(git(root, "show", f"{head}:.architecture/policy.json")).hexdigest())
    return result


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        # API metadata should not redirect. Never forward credentials to another host.
        raise Blocked("unexpected GitHub API redirect")


class GitHub:
    def __init__(self, repo):
        require(re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repo), "invalid repository")
        self.prefix = "https://api.github.com/repos/" + repo
        self.token = os.environ.get("GH_TOKEN", "")
        require(self.token, "GH_TOKEN with read access is required; no offline approval fallback")

    def get(self, endpoint):
        require(endpoint.startswith("/") and not endpoint.startswith("//"), "invalid API endpoint")
        request = urllib.request.Request(self.prefix + endpoint, headers={
            "Accept": "application/vnd.github+json", "Authorization": "Bearer " + self.token,
            "User-Agent": "iot-delivery-gates", "X-GitHub-Api-Version": "2022-11-28"})
        try:
            with urllib.request.build_opener(NoRedirect()).open(request, timeout=30) as response:
                return strict_json(response.read())
        except urllib.error.HTTPError as e:
            raise Blocked(f"GitHub evidence unavailable ({e.code}) for {endpoint}") from None
        except (OSError, ValueError) as e:
            raise Blocked(f"GitHub evidence unavailable for {endpoint}: {type(e).__name__}") from None

    def pages(self, endpoint, key=None):
        result = []
        separator = "&" if "?" in endpoint else "?"
        for page in range(1, 101):
            value = self.get(f"{endpoint}{separator}per_page=100&page={page}")
            items = value[key] if key else value
            require(isinstance(items, list), "malformed paginated evidence")
            result.extend(items)
            if len(items) < 100:
                return result
        raise Blocked("pagination cap reached; partial evidence is not PASS")


def actor(value):
    require(isinstance(value, dict) and isinstance(value.get("id"), int) and value["id"] > 0
            and isinstance(value.get("login"), str) and value["login"], "unresolved contributor/reviewer identity")
    return value["id"]


def evaluate_reviews(pr, reviews, contributors, permissions):
    head = sha(pr["head"]["sha"])
    disallowed = {actor(pr["user"])} | {actor(a) for a in contributors}
    decisive = {}
    for review in sorted(reviews, key=lambda r: r["id"]):
        state = review.get("state")
        if state in {"APPROVED", "CHANGES_REQUESTED", "DISMISSED"}:
            decisive[actor(review["user"])] = review
    require(not any(r["state"] == "CHANGES_REQUESTED" for r in decisive.values()), "outstanding changes requested")
    approvals = []
    for who, review in decisive.items():
        if review["state"] != "APPROVED" or review.get("commit_id") != head or who in disallowed:
            continue
        if permissions.get(who) not in {"admin", "maintain", "write"}:
            continue
        require(review.get("submitted_at"), "approval lacks submission evidence")
        approvals.append({"review_id": review["id"], "reviewer_id": who, "commit": head})
    require(approvals, "no current-head APPROVE by an independent, write-authorized reviewer")
    return sorted(approvals, key=lambda r: r["review_id"])


def review_evidence(api, pr):
    commits = api.pages(f"/pulls/{pr['number']}/commits")
    require(commits, "empty commit evidence")
    contributors = []
    for commit in commits:
        # Unlinked identities cannot establish contributor independence.
        contributors.extend([commit.get("author"), commit.get("committer")])
    reviews = api.pages(f"/pulls/{pr['number']}/reviews")
    permissions = {}
    for review in reviews:
        user = review["user"]
        who = actor(user)
        if review.get("state") == "APPROVED" and who not in permissions:
            permissions[who] = api.get("/collaborators/" + urllib.parse.quote(user["login"], safe="") + "/permission")["permission"]
    return evaluate_reviews(pr, reviews, contributors, permissions)


EXPECTED = {
    ".github/workflows/architecture-gates.yml": {"Architecture / code-contract"},
    ".github/workflows/yu30-regression.yml": {"YU-30 / canonical-full", "YU-30 / go-diagnostics", "YU-30 / web-diagnostics", "YU-30 / browser-e2e"},
    ".github/workflows/yu31-runtime-smoke.yml": {"runtime-smoke"},
}


def evaluate_run(run, jobs, head, expected):
    require(run.get("head_sha") == head and run.get("event") == "pull_request", "CI evidence is for a different source or event")
    require(run.get("status") == "completed" and run.get("conclusion") == "success", "latest workflow attempt is not successful")
    named = {}
    for job in jobs:
        require(job.get("head_sha") == head, "job SHA mismatch")
        require(job.get("run_attempt") == run["run_attempt"], "stale job attempt")
        require(job["name"] not in named, "ambiguous duplicate job name")
        named[job["name"]] = job
        require(job.get("status") == "completed" and job.get("conclusion") == "success", "failed, skipped or missing job")
    require(expected <= set(named), "required job is missing")
    return {"run_id": run["id"], "attempt": run["run_attempt"], "head": head,
            "jobs": {name: named[name]["id"] for name in sorted(expected)}}


def ci_evidence(api, head):
    runs = api.pages(f"/actions/runs?head_sha={head}", "workflow_runs")
    receipts = {}
    for path, expected in EXPECTED.items():
        matches = [r for r in runs if r.get("path") == path and r.get("event") == "pull_request"]
        require(matches, "missing workflow: " + path)
        latest = max(matches, key=lambda r: (r["id"], r["run_attempt"]))
        jobs = api.pages(f"/actions/runs/{latest['id']}/attempts/{latest['run_attempt']}/jobs", "jobs")
        receipts[path] = evaluate_run(latest, jobs, head, expected)
    return receipts


def remote_task(api, pr):
    head, base = sha(pr["head"]["sha"]), sha(pr["base"]["sha"])
    document = api.get(f"/contents/.architecture/change.json?ref={head}")
    require(document.get("encoding") == "base64", "task content is not readable")
    raw = base64.b64decode(document["content"])
    files = api.pages(f"/pulls/{pr['number']}/files")
    changed = []
    for item in files:
        changed.append(item["filename"])
        if item.get("previous_filename"):
            changed.append(item["previous_filename"])
    result = validate_task(strict_json(raw), base, sorted(set(changed)))
    branch = api.get("/branches/main")
    require(branch["commit"]["sha"] == base, "main advanced after the task base; rebase and rerun")
    result["task_sha256"] = hashlib.sha256(raw).hexdigest()
    return result


def remote_framework(api, pr):
    head = sha(pr["head"]["sha"])
    doc = api.get(f"/contents/.architecture/policy.json?ref={head}")
    policy = strict_json(base64.b64decode(doc["content"]))
    link = api.get(f"/contents/third_party/yunka?ref={head}")
    require(link["sha"] == sha(policy["framework"]), "framework pin readback mismatch")
    return {"framework": link["sha"], "policy_sha256": hashlib.sha256(base64.b64decode(doc["content"])).hexdigest()}


def require_server_enforcement(api):
    branch = api.get("/branches/main")
    require(branch.get("protected") is True, "main is unprotected: executable checks are not a server-side merge lock")
    protection = api.get("/branches/main/protection")
    checks = protection.get("required_status_checks") or {}
    contexts = set(checks.get("contexts", [])) | {c["context"] for c in checks.get("checks", [])}
    required = set().union(*EXPECTED.values()) | {"Architecture / independent-review"}
    require(required <= contexts and checks.get("strict") is True, "required/strict status checks are not fully enforced")
    reviews = protection.get("required_pull_request_reviews") or {}
    require(reviews.get("required_approving_review_count", 0) >= 1 and reviews.get("dismiss_stale_reviews") is True,
            "current-head independent review is not enforced")
    require(protection.get("enforce_admins", {}).get("enabled") is True, "administrator bypass is not disabled")
    return {"mode": "classic-branch-protection", "required_contexts": sorted(required)}


def remote(mode, repo, number):
    api = GitHub(repo)
    pr = api.get(f"/pulls/{number}")
    require(pr.get("state") == "open" and not pr.get("draft"), "PR is not open and ready for review")
    require(pr["base"]["ref"] == "main" and pr["base"]["repo"]["full_name"] == repo, "wrong integration target")
    head, base = sha(pr["head"]["sha"]), sha(pr["base"]["sha"])
    layers = {}
    def capture(name, fn):
        try:
            layers[name] = {"status": "PASS", "evidence": fn()}
        except (Blocked, KeyError, TypeError, ValueError) as e:
            layers[name] = {"status": "BLOCKED", "reason": str(e)}
    capture("G3-independent-review", lambda: review_evidence(api, pr))
    if mode == "premerge":
        capture("G2-live-task-base-and-scope", lambda: remote_task(api, pr))
        capture("G4-exact-ci-including-canonical-generation", lambda: ci_evidence(api, head))
        capture("G5-framework-pin", lambda: remote_framework(api, pr))
        capture("G4-server-enforcement", lambda: require_server_enforcement(api))
    reread = api.get(f"/pulls/{number}")
    require(reread.get("state") == "open" and not reread.get("draft")
            and reread["head"]["sha"] == head and reread["base"]["sha"] == base,
            "PR changed during reconciliation; discard all evidence and retry")
    result = {"schema": 1, "repository": repo, "pr": number, "head": head, "base": base,
              "checked_at": dt.datetime.now(dt.timezone.utc).isoformat(), "layers": layers}
    print(json.dumps(result, indent=2, sort_keys=True))
    require(all(x["status"] == "PASS" for x in layers.values()), "delivery gate is BLOCKED; no merge is authorized")


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("mode", choices=["task", "review", "premerge"])
    p.add_argument("--root", type=Path, default=Path.cwd())
    p.add_argument("--base")
    p.add_argument("--repo")
    p.add_argument("--pr", type=int)
    args = p.parse_args()
    try:
        if args.mode == "task":
            result = check_task(args.root, args.base)
            print(json.dumps({"schema": 1, "G2": "PASS", **result}, indent=2, sort_keys=True))
        else:
            require(args.repo and args.pr and args.pr > 0, "repository and PR are required")
            remote(args.mode, args.repo, args.pr)
    except (Blocked, OSError, ValueError, KeyError, TypeError) as e:
        print("DELIVERY GATE BLOCKED: " + str(e), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

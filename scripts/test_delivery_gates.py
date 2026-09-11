import copy
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

import delivery_gates as gate

A = "a" * 40
B = "b" * 40
USER = {"id": 1, "login": "developer"}
REVIEWER = {"id": 2, "login": "reviewer"}


class ContractTests(unittest.TestCase):
    def plan(self):
        return {"schema": 1, "id": "ARCH-01", "kind": "control", "base": A, "goal": "gate introduction",
                "allowed_files": [".architecture/change.json", "tools/archgate/main.go"],
                "invariants": ["framework remains pinned"], "counterexamples": ["forged approval rejected"], "next": "ARCH-02"}

    def test_valid_contract_and_unplanned_file(self):
        gate.validate_task(self.plan(), A, ["tools/archgate/main.go"])
        with self.assertRaises(gate.Blocked):
            gate.validate_task(self.plan(), A, ["unexpected.go"])

    def test_stale_base_empty_and_glob_rejected(self):
        for plan, base, files in [(self.plan(), B, ["tools/archgate/main.go"]), (self.plan(), A, [])]:
            with self.assertRaises(gate.Blocked): gate.validate_task(plan, base, files)
        plan = self.plan(); plan["allowed_files"] = ["**"]
        with self.assertRaises(gate.Blocked): gate.validate_task(plan, A, ["x"])

    def test_self_weakening_and_frozen_sources_rejected(self):
        for changed in ["backend-yunka/internal/delivery/service.go", "third_party/yunka", "backend/internal/new.go"]:
            plan = self.plan(); plan["allowed_files"].append(changed)
            with self.assertRaises(gate.Blocked):
                gate.validate_task(plan, A, ["tools/archgate/main.go", changed])

    def test_duplicate_json_rejected(self):
        with self.assertRaises(gate.Blocked): gate.strict_json('{"base":"a","base":"b"}')

    def test_real_git_task_evidence(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            def git(*args):
                return subprocess.check_output(["git", "-C", temp, *args], stderr=subprocess.DEVNULL).decode().strip()
            git("init"); git("config", "user.name", "test"); git("config", "user.email", "test@example.invalid")
            (root/"seed").write_text("baseline\n"); git("add", "."); git("commit", "-m", "baseline")
            base = git("rev-parse", "HEAD")
            (root/".architecture").mkdir()
            plan = self.plan(); plan["base"] = base; plan["allowed_files"] = [".architecture/change.json", ".architecture/policy.json"]
            (root/".architecture/change.json").write_text(json.dumps(plan))
            (root/".architecture/policy.json").write_text(json.dumps({"baseline":base,"framework":A}))
            git("add", "."); git("commit", "-m", "control")
            result = gate.check_task(root, base)
            self.assertEqual(result["head"], git("rev-parse", "HEAD"))
            (root/"untracked").write_text("not certified")
            with self.assertRaises(gate.Blocked): gate.check_task(root, base)


class ReviewTests(unittest.TestCase):
    def pr(self): return {"head": {"sha": A}, "user": USER}
    def review(self, **changes):
        value = {"id": 10, "state": "APPROVED", "commit_id": A, "user": REVIEWER, "submitted_at": "2026-09-07T00:00:00Z"}
        value.update(changes); return value
    def check(self, reviews, permissions=None, contributors=None):
        return gate.evaluate_reviews(self.pr(), reviews, contributors if contributors is not None else [USER],
                                     permissions if permissions is not None else {2: "write"})

    def test_current_independent_approval(self): self.assertEqual(self.check([self.review()])[0]["reviewer_id"], 2)
    def test_old_self_dismissed_missing_and_read_only(self):
        for reviews, permissions in [([], {2:"write"}), ([self.review(commit_id=B)], {2:"write"}),
                                    ([self.review(user=USER)], {1:"admin"}), ([self.review(state="DISMISSED")], {2:"write"}),
                                    ([self.review()], {2:"read"})]:
            with self.assertRaises(gate.Blocked): self.check(reviews, permissions)
    def test_contributor_cannot_approve_and_unresolved_identity_is_blocking(self):
        for people in [[USER, REVIEWER], [USER, None]]:
            with self.assertRaises(gate.Blocked): self.check([self.review()], contributors=people)
    def test_later_dismissal_and_changes_requested_cannot_be_hidden_by_comment(self):
        for state in ["DISMISSED", "CHANGES_REQUESTED"]:
            with self.assertRaises(gate.Blocked):
                self.check([self.review(), self.review(id=11,state=state), self.review(id=12,state="COMMENTED")])
    def test_old_changes_request_requires_new_decision(self):
        with self.assertRaises(gate.Blocked): self.check([self.review(id=9,state="CHANGES_REQUESTED",commit_id=B)])
        self.check([self.review(id=9,state="CHANGES_REQUESTED",commit_id=B),self.review()])


class CIAndEnforcementTests(unittest.TestCase):
    def run_and_job(self):
        return ({"id":10,"run_attempt":2,"head_sha":A,"event":"pull_request","status":"completed","conclusion":"success"},
                {"id":11,"run_attempt":2,"head_sha":A,"name":"required","status":"completed","conclusion":"success"})
    def test_exact_run(self):
        run,job=self.run_and_job(); self.assertEqual(gate.evaluate_run(run,[job],A,{"required"})["attempt"],2)
    def test_old_skipped_cancelled_missing_and_duplicate_job(self):
        for changes in [{"head_sha":B},{"run_attempt":1},{"conclusion":"skipped"},{"conclusion":"cancelled"},{"name":"other"}]:
            run,job=self.run_and_job(); job.update(changes)
            with self.assertRaises(gate.Blocked): gate.evaluate_run(run,[job],A,{"required"})
        run,job=self.run_and_job()
        with self.assertRaises(gate.Blocked): gate.evaluate_run(run,[job,copy.deepcopy(job)],A,{"required"})
    def test_no_fallback_to_success_while_latest_is_running(self):
        run,job=self.run_and_job();run.update(status="in_progress",conclusion=None)
        with self.assertRaises(gate.Blocked): gate.evaluate_run(run,[job],A,{"required"})
    def test_unprotected_branch_cannot_claim_hard_merge_lock(self):
        class Response:
            def get(self, _): return {"protected":False}
        with self.assertRaises(gate.Blocked): gate.require_server_enforcement(Response())
    def test_delivery_ready_itself_must_be_required(self):
        required = set().union(*gate.EXPECTED.values()) | {"Architecture / independent-review"}
        class Protection:
            def __init__(self, contexts): self.contexts = contexts
            def get(self, endpoint):
                if endpoint == "/branches/main": return {"protected": True}
                return {"required_status_checks": {"strict": True, "contexts": sorted(self.contexts)},
                        "required_pull_request_reviews": {"required_approving_review_count": 1, "dismiss_stale_reviews": True},
                        "enforce_admins": {"enabled": True}}
        with self.assertRaises(gate.Blocked):
            gate.require_server_enforcement(Protection(required))
        gate.require_server_enforcement(Protection(required | {"Architecture / delivery-ready"}))
    def test_pagination_not_first_page_only(self):
        class Pages(gate.GitHub):
            def __init__(self): pass
            def get(self, endpoint): return [1]*100 if endpoint.endswith("&page=1") else [2]
        self.assertEqual(len(Pages().pages("/reviews")),101)


if __name__ == "__main__": unittest.main()

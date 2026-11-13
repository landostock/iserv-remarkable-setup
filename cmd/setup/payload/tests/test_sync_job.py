import base64
import importlib.util
import json
import os
import subprocess
import sys
import tempfile
import unittest
import urllib.error
from datetime import datetime, timedelta, timezone
from pathlib import Path
from unittest import mock


MODULE_PATH = Path(__file__).resolve().parents[1] / "sync_job.py"
SPEC = importlib.util.spec_from_file_location("sync_job", MODULE_PATH)
sync_job = importlib.util.module_from_spec(SPEC)
assert SPEC.loader
sys.modules[SPEC.name] = sync_job
SPEC.loader.exec_module(sync_job)


def jwt_with_expiry(expiry):
    header = base64.urlsafe_b64encode(b'{}').decode().rstrip("=")
    payload = base64.urlsafe_b64encode(json.dumps({"exp": expiry}).encode()).decode().rstrip("=")
    return f"{header}.{payload}.signature"


class SyncJobTests(unittest.TestCase):
    def setUp(self):
        sync_job._last_remarquee_started = 0.0
        sync_job._remarkable_user_token = ""

    def test_safe_label(self):
        self.assertEqual(sync_job.safe_label("Mathe / 10a"), "Mathe _ 10a")

    def test_hash_changes_with_content(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "a.pdf"
            path.write_bytes(b"one")
            first = sync_job.sha256(path)
            path.write_bytes(b"two")
            self.assertNotEqual(first, sync_job.sha256(path))

    def test_eligible_files_filters_type_and_size(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "a.PDF").write_bytes(b"pdf")
            (root / "b.epub").write_bytes(b"epub")
            (root / "c.docx").write_bytes(b"doc")
            names = [path.name for path in sync_job.eligible_files(root, 1, {"pdf", "epub"})]
            self.assertEqual(names, ["a.PDF", "b.epub"])

    def test_eligible_files_respects_selected_types(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "a.pdf").write_bytes(b"pdf")
            (root / "b.epub").write_bytes(b"epub")
            names = [path.name for path in sync_job.eligible_files(root, 1, {"pdf"})]
            self.assertEqual(names, ["a.pdf"])

    def test_manifest_key_changes_with_target(self):
        source = {
            "name": "Deutsch",
            "iserv_path": "Groups/deutsch-q1",
            "remarkable_dir": "/Schule/01 | Deutsch/IServ",
        }
        relative = Path("Texte") / "blatt.pdf"
        first = sync_job.manifest_key(source, relative)
        second = sync_job.manifest_key(source, relative)
        self.assertEqual(first, second)
        changed = {**source, "remarkable_dir": "/Schule/01 | Deutsch/Anders"}
        self.assertNotEqual(first, sync_job.manifest_key(changed, relative))

    def test_target_directory_preserves_subfolders(self):
        source = {"remarkable_dir": "/Schule/01 | Deutsch/IServ"}
        relative = Path("Lektuere") / "Kapitel 1" / "blatt.pdf"
        self.assertEqual(
            sync_job.target_directory(source, relative),
            "/Schule/01 | Deutsch/IServ/Lektuere/Kapitel 1",
        )

    def test_v1_manifest_is_backed_up_and_ignored(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest_path = Path(directory) / "manifest.json"
            manifest_path.write_text('{"version":1,"files":{"old":{"sha256":"abc"}}}', encoding="utf-8")
            result = sync_job.load_manifest(manifest_path)
            self.assertEqual(result, {"version": 2, "files": {}})
            self.assertTrue((Path(directory) / "manifest-pre-v2.json").exists())

    def test_v2_manifest_is_kept(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest_path = Path(directory) / "manifest.json"
            key = "v2:" + "a" * 64
            digest = "b" * 64
            manifest_path.write_text(
                '{"version":2,"files":{"' + key + '":{"sha256":"' + digest + '"}}}',
                encoding="utf-8",
            )
            result = sync_job.load_manifest(manifest_path)
            self.assertIn(key, result["files"])
            self.assertFalse((Path(directory) / "manifest-pre-v2.json").exists())

    def test_corrupt_v2_manifest_fails_closed(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest_path = Path(directory) / "manifest.json"
            manifest_path.write_text('{"version":2,"files":{"bad":{"sha256":"abc"}}}', encoding="utf-8")
            with self.assertRaises(sync_job.SyncError):
                sync_job.load_manifest(manifest_path)

    def test_rate_limit_output_is_detected(self):
        result = subprocess.CompletedProcess(
            ["remarquee"], 1, stdout="", stderr="request failed with status 429"
        )
        self.assertTrue(sync_job.is_rate_limited_result(result))

    def test_non_rate_limit_error_is_not_misclassified(self):
        result = subprocess.CompletedProcess(["remarquee"], 1, stdout="", stderr="file doesn't exist")
        self.assertFalse(sync_job.is_rate_limited_result(result))

    def test_rclone_failures_are_classified_without_exposing_output(self):
        cases = {
            "401 Unauthorized: /private/school": "auth",
            "404 Not Found: /private/school": "source",
            "x509: certificate signed by unknown authority": "certificate",
            "no space left on device": "storage",
            "502 Bad Gateway status code 502": "network",
            "opaque WebDAV failure: /private/school": "provider",
        }
        for output, expected in cases.items():
            result = subprocess.CompletedProcess(["rclone"], 1, stdout="", stderr=output)
            reason = sync_job.classify_rclone_failure(result)
            self.assertEqual(reason, expected)
            self.assertNotIn("/private/school", sync_job.safe_rclone_failure_message(reason))

    def test_rclone_retries_only_transient_network_failures(self):
        network = subprocess.CompletedProcess(["rclone"], 1, stdout="", stderr="status code 502")
        ok = subprocess.CompletedProcess(["rclone"], 0, stdout="", stderr="")
        with mock.patch.object(sync_job, "run", side_effect=[network, network, ok]) as fake_run, mock.patch.object(
            sync_job.time, "sleep"
        ) as fake_sleep:
            result = sync_job.run_rclone(["rclone", "sync", "private-source", "private-target"])
        self.assertEqual(result.returncode, 0)
        self.assertEqual(fake_run.call_count, 3)
        self.assertEqual([call.args[0] for call in fake_sleep.call_args_list], [3, 8])

    def test_rclone_gets_the_exact_encoded_username_and_can_still_read_legacy_secrets(self):
        with mock.patch.dict(os.environ, {"RCLONE_CONFIG_ISERV_USER": "legacy"}, clear=True):
            self.assertIsNone(sync_job.rclone_environment())
            user = '"Test # ?"'
            os.environ["ISERV_WEBDAV_USER_B64"] = base64.b64encode(user.encode()).decode()
            env = sync_job.rclone_environment()
            self.assertEqual(env["RCLONE_CONFIG_ISERV_USER"], user)
            self.assertNotIn("ISERV_WEBDAV_USER_B64", env)
            os.environ["ISERV_WEBDAV_USER_B64"] = "malformed!"
            with self.assertRaises(sync_job.RcloneCommandError) as raised:
                sync_job.rclone_environment()
            self.assertEqual(raised.exception.reason, "auth")
            self.assertNotIn("malformed", str(raised.exception))

    def test_rclone_auth_failure_does_not_retry_or_expose_output(self):
        failure = subprocess.CompletedProcess(
            ["rclone"], 1, stdout="", stderr="401 Unauthorized: /private/school/file.pdf"
        )
        with mock.patch.object(sync_job, "run", return_value=failure) as fake_run:
            with self.assertRaises(sync_job.RcloneCommandError) as raised:
                sync_job.run_rclone(["rclone", "sync", "private-source", "private-target"])
        self.assertEqual(fake_run.call_count, 1)
        self.assertEqual(raised.exception.reason, "auth")
        self.assertNotIn("private", str(raised.exception))

    def test_sync_reports_structured_iserv_failure_instead_of_generic_command_error(self):
        config = {
            "dry_run": True,
            "lookback_days": 30,
            "max_file_mb": 95,
            "extensions": ["pdf"],
            "sources": [{"name": "Test", "iserv_path": "Groups/test", "remarkable_dir": "/Test"}],
        }
        with tempfile.TemporaryDirectory() as directory, mock.patch.multiple(
            sync_job,
            DATA_DIR=Path(directory),
            CONTROL_URL="https://dashboard.example",
            SYNC_API_TOKEN="token",
        ), mock.patch.object(sync_job, "load_sync_config", return_value=config), mock.patch.object(
            sync_job, "install_rmapi_config", return_value=None
        ), mock.patch.object(sync_job, "load_manifest", return_value={"version": 2, "files": {}}), mock.patch.object(
            sync_job, "load_state", return_value={}
        ), mock.patch.object(
            sync_job,
            "collect_candidates",
            side_effect=sync_job.RcloneCommandError("network", sync_job.safe_rclone_failure_message("network")),
        ):
            result = sync_job.sync()
        self.assertEqual(result.outcome, "failed")
        self.assertEqual(result.reason, "iserv_network")
        self.assertTrue(result.dry_run)
        self.assertEqual(result.stats, sync_job.make_stats())
        self.assertNotIn("Befehl fehlgeschlagen", result.message)

    def test_dry_run_probes_remarkable_without_any_cloud_write(self):
        config = {
            "dry_run": True,
            "lookback_days": 30,
            "max_file_mb": 95,
            "extensions": ["pdf"],
            "sources": [{"name": "Test", "iserv_path": "Groups/test", "remarkable_dir": "/Test"}],
        }
        for authorized, expected in ((True, "dry_run"), (False, "remarkable_auth")):
            with self.subTest(authorized=authorized), tempfile.TemporaryDirectory() as directory:
                stats = sync_job.make_stats()
                stats["checked"] = 1
                auth_error = sync_job.RemarqueeCommandError("auth", "Authorization failed")
                with mock.patch.multiple(
                    sync_job,
                    DATA_DIR=Path(directory),
                    CONTROL_URL="https://dashboard.example",
                    SYNC_API_TOKEN="token",
                    load_sync_config=mock.Mock(return_value=config),
                    install_rmapi_config=mock.Mock(),
                    load_manifest=mock.Mock(return_value={"version": 2, "files": {}}),
                    load_state=mock.Mock(return_value={}),
                    collect_candidates=mock.Mock(return_value=([mock.sentinel.candidate], stats)),
                    run_remarquee=mock.Mock(side_effect=None if authorized else auth_error),
                    upload_candidate=mock.Mock(),
                ):
                    result = sync_job.sync()
                    sync_job.run_remarquee.assert_called_once_with(["cloud", "ls", "/", "--non-interactive"])
                    sync_job.upload_candidate.assert_not_called()
                self.assertTrue(result.dry_run)
                self.assertEqual(result.outcome, "success" if authorized else "failed")
                self.assertEqual(result.reason, expected)
                self.assertEqual(result.stats["uploaded"], 0)
                self.assertEqual(result.stats["would_upload"], int(authorized))

    def test_job_waits_for_stored_settings_and_retries_temporary_report_failure(self):
        def temporary_error():
            error = sync_job.SyncError("temporary")
            error.__cause__ = urllib.error.HTTPError("https://example.test", 503, "", {}, None)
            return error

        config = {"dry_run": True}
        report = {"ok": True, "dry_run": True}
        with mock.patch.multiple(
            sync_job,
            CONTROL_URL="https://dashboard.example",
            SYNC_API_TOKEN="token",
            control_request=mock.Mock(side_effect=[temporary_error(), config, temporary_error(), None]),
        ), mock.patch.object(sync_job.time, "sleep") as sleep:
            self.assertEqual(sync_job.load_sync_config(), config)
            sync_job.report_status(report)
            self.assertEqual(sync_job.control_request.call_args_list, [
                mock.call("/api/sync-config"), mock.call("/api/sync-config"),
                mock.call("/api/report", method="POST", value=report),
                mock.call("/api/report", method="POST", value=report),
            ])
            self.assertEqual(sleep.call_args_list, [mock.call(3), mock.call(2)])

    def test_remarquee_failures_are_classified_without_exposing_output(self):
        cases = {
            "entry already exists: /private/school/file.pdf": "conflict",
            "request failed with status 401 Unauthorized": "auth",
            "request failed with status 409 Conflict": "generation",
            "request failed with status 412 Precondition Failed": "generation",
            "request failed with status 428 Precondition Required": "generation",
            "request failed with status 400: invalid root schema": "protocol",
            "software must be updated": "protocol",
            "directory doesn't exist": "target",
            "read: connection reset by peer": "network",
            "invalid PDF document": "document",
            "opaque provider failure": "provider",
        }
        for output, expected in cases.items():
            result = subprocess.CompletedProcess(["remarquee"], 1, stdout="", stderr=output)
            reason = sync_job.classify_remarquee_failure(result)
            self.assertEqual(reason, expected)
            self.assertNotIn("/private/school", sync_job.safe_remarquee_failure_message(reason))

    def test_remarquee_failure_classification_strips_ansi_and_reads_http_status(self):
        result = subprocess.CompletedProcess(
            ["remarquee"],
            1,
            stdout="",
            stderr="\x1b[31mERROR\x1b[0m request failed: HTTP/2 503 Service Unavailable",
        )
        self.assertEqual(sync_job.classify_remarquee_failure(result), "network")

    def test_cold_tree_gets_time_to_complete_without_restarting(self):
        clock = [1000.0]
        def slow_read(command, **options):
            self.assertGreater(options["timeout"], 180)
            clock[0] += 180
            return subprocess.CompletedProcess(command, 0, stdout="private folder names", stderr="")
        with mock.patch.object(sync_job, "run", side_effect=slow_read) as execute, mock.patch.object(
            sync_job, "REMARQUEE_MIN_INTERVAL_SECONDS", 0
        ), mock.patch.object(sync_job.time, "monotonic", side_effect=lambda: clock[0]):
            result = sync_job.run_remarquee(["cloud", "ls", "/", "--non-interactive"])
        self.assertEqual(result.returncode, 0)
        self.assertEqual(execute.call_count, 1)

    def test_pre_upload_tree_failure_is_actionable_and_stops_all_writes(self):
        config = {
            "dry_run": False, "lookback_days": 30, "max_file_mb": 95,
            "extensions": ["pdf"],
            "sources": [{"name": "Test", "iserv_path": "Groups/test", "remarkable_dir": "/Test"}],
        }
        for reason in ("timeout", "auth", "network"):
            with self.subTest(reason=reason), tempfile.TemporaryDirectory() as directory:
                with mock.patch.multiple(
                    sync_job, DATA_DIR=Path(directory),
                    CONTROL_URL="https://dashboard.example", SYNC_API_TOKEN="token",
                    load_sync_config=mock.Mock(return_value=config),
                    install_rmapi_config=mock.Mock(),
                    load_manifest=mock.Mock(return_value={"version": 2, "files": {}}),
                    load_state=mock.Mock(return_value={}),
                    collect_candidates=mock.Mock(return_value=([mock.sentinel.candidate], sync_job.make_stats())),
                    run_remarquee=mock.Mock(side_effect=sync_job.RemarqueeCommandError(reason, "safe read failure")),
                    upload_candidate=mock.Mock(),
                ):
                    result = sync_job.sync()
                    sync_job.upload_candidate.assert_not_called()
                self.assertEqual(result.reason, "remarkable_" + reason)
                self.assertEqual(result.outcome, "failed")
                self.assertEqual(result.stats["uploaded"], 0)
                self.assertEqual(result.stats["deferred"], 1)

    def test_process_timeout_marker_discards_partial_private_output(self):
        with mock.patch.object(sync_job.subprocess, "run", side_effect=subprocess.TimeoutExpired(
            ["remarquee"], 480, output="private document name", stderr="private credential"
        )):
            result = sync_job.run(["remarquee"], check=False, timeout=480)
        self.assertTrue(result.timed_out)
        self.assertEqual(result.stdout, "")
        self.assertEqual(result.stderr, "operation timed out")

    def test_read_retries_share_one_deadline(self):
        clock = [1000.0]
        budgets = []
        def interrupted(command, **options):
            budgets.append(options["timeout"])
            clock[0] += 180
            return subprocess.CompletedProcess(command, 1, stdout="", stderr="HTTP 503")
        with mock.patch.object(sync_job, "run", side_effect=interrupted), mock.patch.object(
            sync_job, "REMARQUEE_MIN_INTERVAL_SECONDS", 0
        ), mock.patch.object(sync_job.time, "monotonic", side_effect=lambda: clock[0]), mock.patch.object(
            sync_job.time, "sleep", side_effect=lambda seconds: clock.__setitem__(0, clock[0] + seconds)
        ):
            with self.assertRaises(sync_job.RemarqueeCommandError) as failure:
                sync_job.run_remarquee(["cloud", "ls", "/"], timeout=300)
        self.assertEqual(budgets, [300, 117])
        self.assertEqual(failure.exception.reason, "timeout")

    def test_expired_read_is_not_restarted_with_another_full_budget(self):
        result = subprocess.CompletedProcess(["remarquee"], 1, stdout="", stderr="operation timed out")
        result.timed_out = True
        with mock.patch.object(sync_job, "run", return_value=result) as execute, mock.patch.object(
            sync_job, "REMARQUEE_MIN_INTERVAL_SECONDS", 0
        ), mock.patch.object(sync_job.time, "sleep") as pause:
            with self.assertRaises(sync_job.RemarqueeCommandError) as failure:
                sync_job.run_remarquee(["cloud", "ls", "/"])
        self.assertEqual(execute.call_count, 1)
        pause.assert_not_called()
        self.assertEqual(failure.exception.diagnostic_code, "RM-READ-TIMEOUT")

    def test_run_remarquee_raises_immediately_on_429(self):
        result = subprocess.CompletedProcess(
            ["remarquee"], 1, stdout="", stderr="request failed with status 429"
        )
        with mock.patch.object(sync_job, "run", return_value=result) as fake_run, mock.patch.object(
            sync_job, "REMARQUEE_MIN_INTERVAL_SECONDS", 0
        ):
            with self.assertRaises(sync_job.RateLimitError):
                sync_job.run_remarquee(["cloud", "stat", "/Schule"])
        self.assertEqual(fake_run.call_count, 1)

    def test_run_remarquee_uses_private_debug_output_for_classification(self):
        result = subprocess.CompletedProcess(["remarquee"], 0, stdout="", stderr="")
        with mock.patch.object(sync_job, "run", return_value=result) as fake_run, mock.patch.object(
            sync_job, "REMARQUEE_MIN_INTERVAL_SECONDS", 0
        ):
            sync_job.run_remarquee(["cloud", "stat", "/Schule"])
        self.assertEqual(
            fake_run.call_args.args[0],
            ["remarquee", "--log-level", "debug", "cloud", "stat", "/Schule"],
        )

    def test_run_remarquee_recovers_from_generation_conflict(self):
        conflict = subprocess.CompletedProcess(
            ["remarquee"], 1, stdout="", stderr="request failed with status 412"
        )
        ok = subprocess.CompletedProcess(["remarquee"], 0, stdout="", stderr="")
        with mock.patch.object(sync_job, "run", side_effect=[conflict, ok]) as fake_run, mock.patch.object(
            sync_job, "clear_rmapi_tree_cache", return_value=True
        ) as clear_cache, mock.patch.object(
            sync_job, "REMARQUEE_MIN_INTERVAL_SECONDS", 0
        ), mock.patch.object(
            sync_job, "REMARQUEE_GENERATION_RETRY_DELAYS_SECONDS", (0,)
        ), mock.patch.object(sync_job.time, "sleep"):
            result = sync_job.run_remarquee(["cloud", "put", "local.pdf", "/Schule"])
        self.assertEqual(result.returncode, 0)
        self.assertEqual(fake_run.call_count, 2)
        clear_cache.assert_called_once_with()

    def test_run_remarquee_refreshes_stale_tree_once_for_protocol_failure(self):
        failed = subprocess.CompletedProcess(
            ["remarquee"], 1, stdout="", stderr="request failed with status 400: invalid root schema"
        )
        with mock.patch.object(sync_job, "run", side_effect=[failed, failed]) as fake_run, mock.patch.object(
            sync_job, "clear_rmapi_tree_cache", return_value=True
        ) as clear_cache, mock.patch.object(
            sync_job, "REMARQUEE_MIN_INTERVAL_SECONDS", 0
        ), mock.patch.object(
            sync_job, "REMARQUEE_PROTOCOL_RETRY_DELAYS_SECONDS", (0,)
        ), mock.patch.object(sync_job.time, "sleep"):
            with self.assertRaises(sync_job.RemarqueeCommandError) as raised:
                sync_job.run_remarquee(["cloud", "put", "local.pdf", "/Schule"])
        self.assertEqual(raised.exception.reason, "protocol")
        self.assertEqual(fake_run.call_count, 2)
        clear_cache.assert_called_once_with()

    def test_run_remarquee_retries_transient_failures_with_bounded_delays(self):
        failed = subprocess.CompletedProcess(
            ["remarquee"], 1, stdout="", stderr="HTTP 503 Service Unavailable"
        )
        ok = subprocess.CompletedProcess(["remarquee"], 0, stdout="", stderr="")
        with mock.patch.object(sync_job, "run", side_effect=[failed, failed, ok]) as fake_run, mock.patch.object(
            sync_job, "REMARQUEE_MIN_INTERVAL_SECONDS", 0
        ), mock.patch.object(
            sync_job, "REMARQUEE_TRANSIENT_RETRY_DELAYS_SECONDS", (3, 8)
        ), mock.patch.object(sync_job.time, "sleep") as fake_sleep:
            result = sync_job.run_remarquee(["cloud", "stat", "/Schule"])
        self.assertEqual(result.returncode, 0)
        self.assertEqual(fake_run.call_count, 3)
        self.assertEqual([call.args[0] for call in fake_sleep.call_args_list], [3, 8])

    def test_run_remarquee_never_blindly_retries_mutating_network_failure(self):
        failed = subprocess.CompletedProcess(
            ["remarquee"], 1, stdout="", stderr="HTTP 503 Service Unavailable"
        )
        with mock.patch.object(sync_job, "run", return_value=failed) as fake_run, mock.patch.object(
            sync_job, "REMARQUEE_MIN_INTERVAL_SECONDS", 0
        ), mock.patch.object(sync_job.time, "sleep") as fake_sleep:
            with self.assertRaises(sync_job.RemarqueeCommandError) as raised:
                sync_job.run_remarquee(["cloud", "put", "local.pdf", "/Schule"])
        self.assertEqual(raised.exception.reason, "network")
        fake_run.assert_called_once()
        fake_sleep.assert_not_called()

    def test_clear_rmapi_tree_cache_only_removes_the_cache_file(self):
        with tempfile.TemporaryDirectory() as directory, mock.patch.dict(
            sync_job.os.environ, {"XDG_CACHE_HOME": directory}
        ):
            cache_path = Path(directory) / "rmapi" / "tree.cache"
            sibling = cache_path.with_name("keep.txt")
            cache_path.parent.mkdir(parents=True)
            cache_path.write_text("stale", encoding="utf-8")
            sibling.write_text("keep", encoding="utf-8")
            self.assertTrue(sync_job.clear_rmapi_tree_cache())
            self.assertFalse(cache_path.exists())
            self.assertTrue(sibling.exists())

    def test_provider_failure_does_not_recommend_reauthentication(self):
        message = sync_job.safe_remarquee_failure_message("provider")
        self.assertNotIn("Verbinde reMarkable erneut", message)
        self.assertIn("Diagnosebericht", message)

    def test_rmapi_token_is_read_without_exposing_other_config(self):
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "rmapi.conf"
            config_path.write_text(
                'devicetoken: "device-secret"\nusertoken: user-secret\n',
                encoding="utf-8",
            )
            with mock.patch.object(sync_job, "RMAPI_CONFIG_PATH", config_path):
                self.assertEqual(sync_job.rmapi_token("devicetoken"), "device-secret")
                self.assertEqual(sync_job.rmapi_token("usertoken"), "user-secret")

    def test_rmapi_token_rejects_malformed_value_without_exposing_it(self):
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "rmapi.conf"
            config_path.write_text("devicetoken: private token with spaces\n", encoding="utf-8")
            with mock.patch.object(sync_job, "RMAPI_CONFIG_PATH", config_path):
                with self.assertRaises(sync_job.RemarqueeCommandError) as raised:
                    sync_job.rmapi_token("devicetoken")
            self.assertEqual(raised.exception.reason, "auth")
            self.assertNotIn("private", str(raised.exception))

    def test_remarkable_user_token_reuses_valid_persisted_session(self):
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "rmapi.conf"
            token = jwt_with_expiry(int(sync_job.time.time()) + 3600)
            config_path.write_text(
                f"devicetoken: device-secret\nusertoken: {token}\n",
                encoding="utf-8",
            )
            with mock.patch.object(sync_job, "RMAPI_CONFIG_PATH", config_path), mock.patch.object(
                sync_job.urllib.request, "build_opener"
            ) as build_opener:
                self.assertEqual(sync_job.remarkable_user_token(), token)
            build_opener.assert_not_called()

    def test_remarkable_user_token_refreshes_at_current_auth_host(self):
        token = jwt_with_expiry(int(sync_job.time.time()) + 3600)

        class FakeResponse:
            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return False

            def read(self, _limit):
                return token.encode("ascii")

        opener = mock.Mock()
        opener.open.return_value = FakeResponse()
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "rmapi.conf"
            config_path.write_text("devicetoken: device-secret\n", encoding="utf-8")
            with mock.patch.object(sync_job, "RMAPI_CONFIG_PATH", config_path), mock.patch.object(
                sync_job.urllib.request, "build_opener", return_value=opener
            ):
                self.assertEqual(sync_job.remarkable_user_token(force_refresh=True), token)

        request = opener.open.call_args.args[0]
        self.assertEqual(request.full_url, "https://webapp.cloud.remarkable.com/token/json/2/user/new")
        self.assertEqual(request.get_method(), "POST")
        self.assertEqual(request.headers["Content-type"], "application/json")

    def test_remarkable_user_token_refreshes_expired_persisted_session(self):
        expired = jwt_with_expiry(int(sync_job.time.time()) - 60)
        refreshed = jwt_with_expiry(int(sync_job.time.time()) + 3600)

        class FakeResponse:
            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return False

            def read(self, _limit):
                return refreshed.encode("ascii")

        opener = mock.Mock()
        opener.open.return_value = FakeResponse()
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "rmapi.conf"
            config_path.write_text(
                f"devicetoken: device-secret\nusertoken: {expired}\n",
                encoding="utf-8",
            )
            with mock.patch.object(sync_job, "RMAPI_CONFIG_PATH", config_path), mock.patch.object(
                sync_job.urllib.request, "build_opener", return_value=opener
            ):
                self.assertEqual(sync_job.remarkable_user_token(), refreshed)

        opener.open.assert_called_once()

    def test_remarkable_user_token_uses_bounded_compatible_auth_fallback(self):
        token = jwt_with_expiry(int(sync_job.time.time()) + 3600)

        class FakeResponse:
            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return False

            def read(self, _limit):
                return token.encode("ascii")

        first_url = sync_job.REMARKABLE_AUTH_URLS[0]
        opener = mock.Mock()
        opener.open.side_effect = [
            urllib.error.HTTPError(first_url, 404, "not found", {}, None),
            FakeResponse(),
        ]
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "rmapi.conf"
            config_path.write_text("devicetoken: device-secret\n", encoding="utf-8")
            with mock.patch.object(sync_job, "RMAPI_CONFIG_PATH", config_path), mock.patch.object(
                sync_job.urllib.request, "build_opener", return_value=opener
            ):
                self.assertEqual(sync_job.remarkable_user_token(force_refresh=True), token)

        self.assertEqual(opener.open.call_count, 2)
        self.assertEqual(
            opener.open.call_args_list[1].args[0].full_url,
            "https://webapp-prod.cloud.remarkable.engineering/token/json/2/user/new",
        )

    def test_remarkable_user_token_does_not_fallback_after_auth_rejection(self):
        first_url = sync_job.REMARKABLE_AUTH_URLS[0]
        opener = mock.Mock()
        opener.open.side_effect = urllib.error.HTTPError(first_url, 401, "unauthorized", {}, None)
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "rmapi.conf"
            config_path.write_text("devicetoken: device-secret\n", encoding="utf-8")
            with mock.patch.object(sync_job, "RMAPI_CONFIG_PATH", config_path), mock.patch.object(
                sync_job.urllib.request, "build_opener", return_value=opener
            ):
                with self.assertRaises(sync_job.RemarqueeCommandError) as raised:
                    sync_job.remarkable_user_token(force_refresh=True)

        self.assertEqual(raised.exception.reason, "auth")
        self.assertEqual(raised.exception.diagnostic_code, "RM-SESSION-HTTP-401")
        opener.open.assert_called_once()

    def test_jwt_expiry_rejects_invalid_base64url_segment(self):
        self.assertIsNone(sync_job.jwt_expiry("header.invalid+segment.signature"))

    def test_remote_directory_id_requires_a_valid_directory(self):
        valid_id = "01234567-89ab-cdef-0123-456789abcdef"
        ok = subprocess.CompletedProcess(
            ["remarquee"], 0, stdout=f"id={valid_id}\nis_dir=true\n", stderr=""
        )
        invalid = subprocess.CompletedProcess(
            ["remarquee"], 0, stdout="id=not-a-uuid\nis_dir=true\n", stderr=""
        )
        with mock.patch.object(sync_job, "run_remarquee", return_value=ok):
            self.assertEqual(sync_job.remote_directory_id("/Schule"), valid_id)
        with mock.patch.object(sync_job, "run_remarquee", return_value=invalid):
            with self.assertRaises(sync_job.RemarqueeCommandError) as raised:
                sync_job.remote_directory_id("/Schule")
        self.assertEqual(raised.exception.reason, "target")

    def test_remote_document_exists_refreshes_cache_and_handles_known_missing(self):
        missing = subprocess.CompletedProcess(
            ["remarquee"], 1, stdout="", stderr="file doesn't exist"
        )
        with mock.patch.object(sync_job, "clear_rmapi_tree_cache") as clear_cache, mock.patch.object(
            sync_job, "run_remarquee", return_value=missing
        ) as run_remarquee:
            exists = sync_job.remote_document_exists("/Schule", Path("blatt.pdf"))
        self.assertFalse(exists)
        clear_cache.assert_called_once_with()
        self.assertEqual(
            run_remarquee.call_args.args[0],
            ["cloud", "stat", "/Schule/blatt", "--non-interactive"],
        )

    def test_native_upload_once_sends_bounded_validated_request(self):
        document_id = "01234567-89ab-cdef-0123-456789abcdef"
        document_hash = "a" * 64

        class FakeResponse:
            status = 200

            def read(self, _limit):
                return json.dumps({"docID": document_id, "hash": document_hash}).encode()

            def getheader(self, _name, default=""):
                return default

        class FakeConnection:
            instance = None

            def __init__(self, host, port=None, timeout=None):
                self.host = host
                self.port = port
                self.timeout = timeout
                self.headers = {}
                self.body = bytearray()
                self.closed = False
                FakeConnection.instance = self

            def putrequest(self, method, path, skip_accept_encoding=False):
                self.request = (method, path, skip_accept_encoding)

            def putheader(self, name, value):
                self.headers[name] = value

            def endheaders(self):
                return None

            def send(self, block):
                self.body.extend(block)

            def getresponse(self):
                return FakeResponse()

            def close(self):
                self.closed = True

        with tempfile.TemporaryDirectory() as directory:
            local = Path(directory) / "Arbeitsblatt.pdf"
            local.write_bytes(b"pdf-data")
            with mock.patch.object(sync_job.http.client, "HTTPSConnection", FakeConnection):
                sync_job.native_upload_once(local, document_id, "private-token")

        connection = FakeConnection.instance
        self.assertEqual(connection.request, ("POST", "/doc/v2/files", True))
        self.assertEqual(connection.body, b"pdf-data")
        self.assertEqual(connection.headers["Content-Type"], "application/pdf")
        self.assertEqual(connection.headers["rm-source"], "RoR-Browser")
        metadata = json.loads(base64.b64decode(connection.headers["rm-meta"]))
        self.assertEqual(metadata, {"file_name": "Arbeitsblatt", "parent": document_id})
        self.assertEqual(connection.headers["Authorization"], "Bearer private-token")
        self.assertTrue(connection.closed)

    def test_native_upload_rejects_malformed_success_response(self):
        class FakeResponse:
            status = 200

            def read(self, _limit):
                return b'{"docID":"invalid","hash":"private-provider-output"}'

            def getheader(self, _name, default=""):
                return default

        connection = mock.Mock()
        connection.getresponse.return_value = FakeResponse()
        with tempfile.TemporaryDirectory() as directory:
            local = Path(directory) / "a.pdf"
            local.write_bytes(b"pdf")
            with mock.patch.object(sync_job.http.client, "HTTPSConnection", return_value=connection):
                with self.assertRaises(sync_job.RemarqueeCommandError) as raised:
                    sync_job.native_upload_once(local, "", "private-token")
        self.assertEqual(raised.exception.reason, "provider")
        self.assertEqual(raised.exception.diagnostic_code, "RM-UPLOAD-RESPONSE")
        self.assertTrue(raised.exception.ambiguous_write)
        self.assertNotIn("private", str(raised.exception))

    def test_native_upload_accepts_opaque_document_id_and_uppercase_hash(self):
        class FakeResponse:
            status = 201

            def read(self, _limit):
                return json.dumps({"docID": "provider-document-id", "hash": "A" * 64}).encode()

            def getheader(self, _name, default=""):
                return default

        connection = mock.Mock()
        connection.getresponse.return_value = FakeResponse()
        with tempfile.TemporaryDirectory() as directory:
            local = Path(directory) / "a.pdf"
            local.write_bytes(b"pdf")
            with mock.patch.object(sync_job.http.client, "HTTPSConnection", return_value=connection):
                sync_job.native_upload_once(local, "", "private-token")

    def test_native_upload_follows_only_bounded_remarkable_https_redirects(self):
        document_id = "01234567-89ab-cdef-0123-456789abcdef"
        document_hash = "a" * 64

        class FakeResponse:
            def __init__(self, status, location="", payload=b""):
                self.status = status
                self.location = location
                self.payload = payload

            def read(self, _limit):
                return self.payload

            def getheader(self, name, default=""):
                return self.location if name == "Location" else default

        responses = [
            FakeResponse(308, "https://eu.tectonic.remarkable.com/doc/v2/files"),
            FakeResponse(200, payload=json.dumps({"docID": document_id, "hash": document_hash}).encode()),
        ]
        connections = []

        class FakeConnection:
            def __init__(self, host, port=None, timeout=None):
                self.host = host
                self.port = port
                self.timeout = timeout
                self.body = bytearray()
                connections.append(self)

            def putrequest(self, method, path, skip_accept_encoding=False):
                self.request = (method, path, skip_accept_encoding)

            def putheader(self, _name, _value):
                return None

            def endheaders(self):
                return None

            def send(self, block):
                self.body.extend(block)

            def getresponse(self):
                return responses.pop(0)

            def close(self):
                return None

        with tempfile.TemporaryDirectory() as directory:
            local = Path(directory) / "a.pdf"
            local.write_bytes(b"pdf")
            with mock.patch.object(sync_job.http.client, "HTTPSConnection", FakeConnection):
                sync_job.native_upload_once(local, "", "private-token")
        self.assertEqual([connection.host for connection in connections], [
            "internal.cloud.remarkable.com",
            "eu.tectonic.remarkable.com",
        ])
        self.assertTrue(all(connection.body == b"pdf" for connection in connections))

    def test_native_upload_rejects_redirect_to_foreign_host_before_sending_token(self):
        class FakeResponse:
            status = 308

            def read(self, _limit):
                return b""

            def getheader(self, name, default=""):
                return "https://attacker.example/upload" if name == "Location" else default

        connection = mock.Mock()
        connection.getresponse.return_value = FakeResponse()
        with tempfile.TemporaryDirectory() as directory:
            local = Path(directory) / "a.pdf"
            local.write_bytes(b"pdf")
            with mock.patch.object(sync_job.http.client, "HTTPSConnection", return_value=connection) as factory:
                with self.assertRaises(sync_job.RemarqueeCommandError) as raised:
                    sync_job.native_upload_once(local, "", "private-token")
        self.assertEqual(raised.exception.reason, "provider")
        factory.assert_called_once()

    def test_native_upload_gateway_failure_requires_write_reconciliation(self):
        connection = mock.Mock()
        connection.getresponse.return_value.status = 503
        connection.getresponse.return_value.read.return_value = b"private provider detail"
        connection.getresponse.return_value.getheader.return_value = ""
        with tempfile.TemporaryDirectory() as directory:
            local = Path(directory) / "fixture.pdf"
            local.write_bytes(b"%PDF-1.4")
            with mock.patch.object(sync_job.http.client, "HTTPSConnection", return_value=connection):
                with self.assertRaises(sync_job.RemarqueeCommandError) as raised:
                    sync_job.native_upload_once(local, "", "fixture-token")
        self.assertTrue(raised.exception.ambiguous_write)
        self.assertEqual(raised.exception.reason, "network")
        self.assertEqual(raised.exception.diagnostic_code, "RM-UPLOAD-HTTP-503")
        self.assertNotIn("private", str(raised.exception))
        connection.close.assert_called_once()

    def test_native_upload_checks_conflict_before_sending(self):
        with mock.patch.object(sync_job, "remote_document_exists", return_value=True), mock.patch.object(
            sync_job, "native_upload_once"
        ) as upload:
            with self.assertRaises(sync_job.RemarqueeCommandError) as raised:
                sync_job.native_upload_file(Path("blatt.pdf"), "/Schule")
        self.assertEqual(raised.exception.reason, "conflict")
        upload.assert_not_called()

    def test_native_upload_confirms_ambiguous_success_before_retry(self):
        interrupted = sync_job.RemarqueeCommandError(
            "provider",
            sync_job.safe_remarquee_failure_message("provider"),
            diagnostic_code="RM-UPLOAD-RESPONSE",
            ambiguous_write=True,
        )
        with mock.patch.object(
            sync_job, "remote_document_exists", side_effect=[False, False, True]
        ) as exists, mock.patch.object(
            sync_job, "remote_directory_id", return_value="01234567-89ab-cdef-0123-456789abcdef"
        ), mock.patch.object(
            sync_job, "remarkable_user_token", return_value="private-token"
        ), mock.patch.object(
            sync_job, "native_upload_once", side_effect=interrupted
        ) as upload, mock.patch.object(
            sync_job, "REMARQUEE_TRANSIENT_RETRY_DELAYS_SECONDS", (0,)
        ), mock.patch.object(sync_job.time, "sleep"):
            sync_job.native_upload_file(Path("blatt.pdf"), "/Schule")
        self.assertEqual(exists.call_count, 3)
        upload.assert_called_once()

    def test_upload_fallback_is_limited_to_protocol_and_provider_failures(self):
        for reason in ("protocol", "provider"):
            failure = sync_job.RemarqueeCommandError(
                reason, sync_job.safe_remarquee_failure_message(reason)
            )
            with mock.patch.object(sync_job, "run_remarquee", side_effect=failure), mock.patch.object(
                sync_job, "native_upload_file"
            ) as fallback:
                sync_job.upload_file(Path("blatt.pdf"), "/Schule")
            fallback.assert_called_once_with(Path("blatt.pdf"), "/Schule")

        for reason in ("auth", "document", "network", "target"):
            failure = sync_job.RemarqueeCommandError(
                reason, sync_job.safe_remarquee_failure_message(reason)
            )
            with mock.patch.object(sync_job, "run_remarquee", side_effect=failure), mock.patch.object(
                sync_job, "native_upload_file"
            ) as fallback:
                with self.assertRaises(sync_job.RemarqueeCommandError):
                    sync_job.upload_file(Path("blatt.pdf"), "/Schule")
            fallback.assert_not_called()

    def test_directory_cache_avoids_repeated_stat_calls(self):
        ok = subprocess.CompletedProcess(["remarquee"], 0, stdout="", stderr="")
        calls = []

        def fake_run_remarquee(args, check=True):
            calls.append(tuple(args))
            return ok

        ensured = set()
        with mock.patch.object(sync_job, "run_remarquee", side_effect=fake_run_remarquee):
            sync_job.ensure_remote_directory("/Schule/01 | Deutsch/IServ", ensured)
            sync_job.ensure_remote_directory("/Schule/01 | Deutsch/IServ", ensured)

        self.assertEqual(
            calls,
            [
                ("cloud", "stat", "/Schule", "--non-interactive"),
                ("cloud", "stat", "/Schule/01 | Deutsch", "--non-interactive"),
                ("cloud", "stat", "/Schule/01 | Deutsch/IServ", "--non-interactive"),
            ],
        )

    def test_directory_creation_stops_immediately_on_rate_limit(self):
        calls = []

        def fake_run_remarquee(args, check=True):
            calls.append(tuple(args))
            raise sync_job.RateLimitError("429")

        with mock.patch.object(sync_job, "run_remarquee", side_effect=fake_run_remarquee):
            with self.assertRaises(sync_job.RateLimitError):
                sync_job.ensure_remote_directory("/Schule/Deutsch", set())
        self.assertEqual(len(calls), 1)

    def test_cooldown_remaining_seconds(self):
        now = datetime(2026, 8, 19, 0, 0, tzinfo=timezone.utc)
        state = {"cooldown_until": (now + timedelta(minutes=30)).isoformat()}
        self.assertEqual(sync_job.cooldown_remaining_seconds(state, now), 1800)

    def test_default_upload_limit_is_conservative(self):
        self.assertGreaterEqual(sync_job.MAX_UPLOADS_PER_RUN, 1)
        self.assertEqual(sync_job.MAX_UPLOADS_PER_RUN, 5)

    def test_sync_upload_limit_defers_remaining_candidates(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source_dir = root / "source"
            source_dir.mkdir()
            for index in range(7):
                (source_dir / f"{index}.pdf").write_bytes(f"pdf-{index}".encode())

            config = {
                "dry_run": False,
                "lookback_days": 30,
                "max_file_mb": 95,
                "extensions": ["pdf"],
                "sources": [{
                    "name": "Deutsch",
                    "iserv_path": "Groups/deutsch",
                    "remarkable_dir": "/Schule/01 | Deutsch/IServ",
                }],
            }
            ok = subprocess.CompletedProcess(["remarquee"], 0, stdout="", stderr="")
            calls = []

            def fake_remarquee(args, check=True):
                calls.append(tuple(args))
                return ok

            with mock.patch.multiple(
                sync_job,
                DATA_DIR=root,
                MANIFEST_PATH=root / "manifest.json",
                STATE_PATH=root / "sync_state.json",
                RMAPI_CONFIG_PATH=root / "rmapi.conf",
                CONTROL_URL="https://dashboard.example",
                SYNC_API_TOKEN="token",
                MAX_UPLOADS_PER_RUN=5,
            ), mock.patch.object(sync_job, "load_sync_config", return_value=config), mock.patch.object(
                sync_job, "install_rmapi_config", return_value=None
            ), mock.patch.object(sync_job, "download_source", return_value=source_dir), mock.patch.object(
                sync_job, "run_remarquee", side_effect=fake_remarquee
            ):
                result = sync_job.sync()

            put_calls = [call for call in calls if call[:2] == ("cloud", "put")]
            self.assertEqual(len(put_calls), 5)
            self.assertEqual(result.stats["uploaded"], 5)
            self.assertEqual(result.stats["deferred"], 2)
            self.assertEqual(result.stats["failed"], 0)
            self.assertEqual(result.outcome, "partial")
            self.assertEqual(result.reason, "upload_limit")

    def test_sync_preserves_existing_remote_document_with_versioned_copy(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source_dir = root / "source"
            source_dir.mkdir()
            (source_dir / "blatt.pdf").write_bytes(b"pdf")
            config = {
                "dry_run": False,
                "lookback_days": 30,
                "max_file_mb": 95,
                "extensions": ["pdf"],
                "sources": [{
                    "name": "Deutsch",
                    "iserv_path": "Groups/deutsch",
                    "remarkable_dir": "/Schule/Deutsch",
                }],
            }
            ok = subprocess.CompletedProcess(["remarquee"], 0, stdout="", stderr="")
            put_paths = []

            def fake_remarquee(args, check=True):
                if args[:2] == ["cloud", "put"]:
                    put_paths.append(Path(args[2]).name)
                    if len(put_paths) == 1:
                        raise sync_job.RemarqueeCommandError("conflict", sync_job.safe_remarquee_failure_message("conflict"))
                return ok

            with mock.patch.multiple(
                sync_job,
                DATA_DIR=root,
                MANIFEST_PATH=root / "manifest.json",
                STATE_PATH=root / "sync_state.json",
                RMAPI_CONFIG_PATH=root / "rmapi.conf",
                CONTROL_URL="https://dashboard.example",
                SYNC_API_TOKEN="token",
                MAX_UPLOADS_PER_RUN=5,
            ), mock.patch.object(sync_job, "load_sync_config", return_value=config), mock.patch.object(
                sync_job, "install_rmapi_config", return_value=None
            ), mock.patch.object(sync_job, "download_source", return_value=source_dir), mock.patch.object(
                sync_job, "run_remarquee", side_effect=fake_remarquee
            ):
                result = sync_job.sync()

            self.assertEqual(len(put_paths), 2)
            self.assertEqual(put_paths[0], "blatt.pdf")
            self.assertIn("aktualisiert", put_paths[1])
            self.assertEqual(result.stats["uploaded"], 1)
            self.assertEqual(result.stats["failed"], 0)
            self.assertEqual(result.outcome, "success")

    def test_conflict_retry_never_exceeds_upload_attempt_limit(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source_dir = root / "source"
            source_dir.mkdir()
            (source_dir / "blatt.pdf").write_bytes(b"pdf")
            config = {
                "dry_run": False,
                "lookback_days": 30,
                "max_file_mb": 95,
                "extensions": ["pdf"],
                "sources": [{
                    "name": "Deutsch",
                    "iserv_path": "Groups/deutsch",
                    "remarkable_dir": "/Schule/Deutsch",
                }],
            }
            ok = subprocess.CompletedProcess(["remarquee"], 0, stdout="", stderr="")
            put_count = 0

            def fake_remarquee(args, check=True):
                nonlocal put_count
                if args[:2] == ["cloud", "put"]:
                    put_count += 1
                    raise sync_job.RemarqueeCommandError("conflict", sync_job.safe_remarquee_failure_message("conflict"))
                return ok

            with mock.patch.multiple(
                sync_job,
                DATA_DIR=root,
                MANIFEST_PATH=root / "manifest.json",
                STATE_PATH=root / "sync_state.json",
                RMAPI_CONFIG_PATH=root / "rmapi.conf",
                CONTROL_URL="https://dashboard.example",
                SYNC_API_TOKEN="token",
                MAX_UPLOADS_PER_RUN=1,
            ), mock.patch.object(sync_job, "load_sync_config", return_value=config), mock.patch.object(
                sync_job, "install_rmapi_config", return_value=None
            ), mock.patch.object(sync_job, "download_source", return_value=source_dir), mock.patch.object(
                sync_job, "run_remarquee", side_effect=fake_remarquee
            ):
                result = sync_job.sync()

            self.assertEqual(put_count, 1)
            self.assertEqual(result.stats["uploaded"], 0)
            self.assertEqual(result.stats["failed"], 0)
            self.assertEqual(result.stats["deferred"], 1)
            self.assertEqual(result.outcome, "partial")
            self.assertEqual(result.reason, "upload_limit")

    def test_sync_stops_after_first_systematic_upload_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source_dir = root / "source"
            source_dir.mkdir()
            for index in range(7):
                (source_dir / f"{index}.pdf").write_bytes(f"pdf-{index}".encode())
            config = {
                "dry_run": False,
                "lookback_days": 30,
                "max_file_mb": 95,
                "extensions": ["pdf"],
                "sources": [{
                    "name": "Deutsch",
                    "iserv_path": "Groups/deutsch",
                    "remarkable_dir": "/Schule/Deutsch",
                }],
            }
            ok = subprocess.CompletedProcess(["remarquee"], 0, stdout="", stderr="")
            put_count = 0

            def fake_remarquee(args, check=True):
                nonlocal put_count
                if args[:2] == ["cloud", "put"]:
                    put_count += 1
                    raise sync_job.RemarqueeCommandError("auth", sync_job.safe_remarquee_failure_message("auth"))
                return ok

            with mock.patch.multiple(
                sync_job,
                DATA_DIR=root,
                MANIFEST_PATH=root / "manifest.json",
                STATE_PATH=root / "sync_state.json",
                RMAPI_CONFIG_PATH=root / "rmapi.conf",
                CONTROL_URL="https://dashboard.example",
                SYNC_API_TOKEN="token",
                MAX_UPLOADS_PER_RUN=5,
            ), mock.patch.object(sync_job, "load_sync_config", return_value=config), mock.patch.object(
                sync_job, "install_rmapi_config", return_value=None
            ), mock.patch.object(sync_job, "download_source", return_value=source_dir), mock.patch.object(
                sync_job, "run_remarquee", side_effect=fake_remarquee
            ):
                result = sync_job.sync()

            self.assertEqual(put_count, 1)
            self.assertEqual(result.stats["failed"], 1)
            self.assertEqual(result.stats["deferred"], 6)
            self.assertEqual(result.outcome, "failed")
            self.assertEqual(result.reason, "remarkable_auth")

    def test_sync_stops_all_remarkable_calls_after_first_429(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source_dir = root / "source"
            source_dir.mkdir()
            for index in range(3):
                (source_dir / f"{index}.pdf").write_bytes(f"pdf-{index}".encode())

            config = {
                "dry_run": False,
                "lookback_days": 30,
                "max_file_mb": 95,
                "extensions": ["pdf"],
                "sources": [{
                    "name": "Deutsch",
                    "iserv_path": "Groups/deutsch",
                    "remarkable_dir": "/Schule/01 | Deutsch/IServ",
                }],
            }
            ok = subprocess.CompletedProcess(["remarquee"], 0, stdout="", stderr="")
            calls = []
            put_count = 0

            def fake_remarquee(args, check=True):
                nonlocal put_count
                calls.append(tuple(args))
                if args[:2] == ["cloud", "put"]:
                    put_count += 1
                    if put_count == 2:
                        raise sync_job.RateLimitError("429")
                return ok

            with mock.patch.multiple(
                sync_job,
                DATA_DIR=root,
                MANIFEST_PATH=root / "manifest.json",
                STATE_PATH=root / "sync_state.json",
                RMAPI_CONFIG_PATH=root / "rmapi.conf",
                CONTROL_URL="https://dashboard.example",
                SYNC_API_TOKEN="token",
                MAX_UPLOADS_PER_RUN=5,
            ), mock.patch.object(sync_job, "load_sync_config", return_value=config), mock.patch.object(
                sync_job, "install_rmapi_config", return_value=None
            ), mock.patch.object(sync_job, "download_source", return_value=source_dir), mock.patch.object(
                sync_job, "run_remarquee", side_effect=fake_remarquee
            ):
                result = sync_job.sync()

            put_calls = [call for call in calls if call[:2] == ("cloud", "put")]
            self.assertEqual(len(put_calls), 2)
            self.assertEqual(result.stats["uploaded"], 1)
            self.assertEqual(result.stats["deferred"], 2)
            self.assertEqual(result.stats["failed"], 0)
            self.assertEqual(result.outcome, "partial")
            self.assertEqual(result.reason, "rate_limit")
            self.assertTrue((root / "sync_state.json").exists())

    def test_remote_stat_non_missing_error_never_attempts_mkdir(self):
        failure = subprocess.CompletedProcess(
            ["remarquee"], 1, stdout="", stderr="authentication failed"
        )
        calls = []

        def fake_run(args, check=True):
            calls.append(tuple(args))
            return failure

        with mock.patch.object(sync_job, "run_remarquee", side_effect=fake_run):
            with self.assertRaises(sync_job.SyncError):
                sync_job.ensure_remote_directory("/Schule/Deutsch", set())
        self.assertEqual(calls, [("cloud", "stat", "/Schule", "--non-interactive")])

    def test_remote_stat_missing_error_creates_directory(self):
        missing = subprocess.CompletedProcess(
            ["remarquee"], 1, stdout="", stderr="file doesn't exist"
        )
        ok = subprocess.CompletedProcess(["remarquee"], 0, stdout="", stderr="")
        calls = []

        def fake_run(args, check=True):
            calls.append(tuple(args))
            return missing if args[:2] == ["cloud", "stat"] else ok

        with mock.patch.object(sync_job, "run_remarquee", side_effect=fake_run):
            sync_job.ensure_remote_directory("/Schule", set())
        self.assertEqual(calls, [
            ("cloud", "stat", "/Schule", "--non-interactive"),
            ("cloud", "mkdir", "/Schule", "--non-interactive"),
        ])

    def test_live_rmapi_missing_spelling_creates_each_missing_ancestor_once(self):
        calls = []
        created = set()
        def command(args, check=True):
            calls.append(tuple(args))
            action, path = args[1:3]
            if action == "stat" and path not in created:
                return subprocess.CompletedProcess(args, 1, stdout="", stderr=f"Error: entry '{Path(path).name}' doesnt exist\n")
            if action == "mkdir":
                created.add(path)
            return subprocess.CompletedProcess(args, 0, stdout="is_dir=true\n", stderr="")
        known = set()
        with mock.patch.object(sync_job, "run_remarquee", side_effect=command):
            sync_job.ensure_remote_directory("/Schule/O'Connor & Deutsch/IServ", known)
            sync_job.ensure_remote_directory("/Schule/O'Connor & Deutsch/IServ", known)
        self.assertEqual(len([c for c in calls if c[1] == "mkdir"]), 3)
        self.assertEqual(len(calls), 6)
        self.assertEqual(known, created)

    def test_missing_detection_accepts_only_the_actual_missing_entry_line(self):
        for text in ["entry 'IServ' doesnt exist", "Error: entry 'O'Connor' doesnt exist\n", "\x1b[31mError: entry 'Δεutsch' doesnt exist\x1b[0m\n"]:
            self.assertTrue(sync_job.is_missing_remote_result(subprocess.CompletedProcess([], 1, stdout="", stderr=text)))
            self.assertFalse(sync_job.is_missing_remote_result(subprocess.CompletedProcess([], 0, stdout="", stderr=text)))
        for text in ["HTTP 401: {\"error\":\"entry 'IServ' doesnt exist\"}", "HTTP 404 Not Found", "config file does not exist", "open private-cache: no such file", "server unavailable", "invalid token", "request failed with status 503"]:
            self.assertFalse(sync_job.is_missing_remote_result(subprocess.CompletedProcess([], 1, stdout="", stderr=text)))

    def test_directory_creation_race_requires_confirmed_directory(self):
        missing = subprocess.CompletedProcess([], 1, stdout="", stderr="entry 'IServ' doesnt exist")
        for valid in (True, False):
            confirmed = subprocess.CompletedProcess([], 0, stdout=f"is_dir={'true' if valid else 'false'}\n", stderr="")
            conflict = sync_job.RemarqueeCommandError("conflict", "already exists")
            known = set()
            with mock.patch.object(sync_job, "run_remarquee", side_effect=[missing, conflict, confirmed]) as command:
                if valid:
                    sync_job.ensure_remote_directory("/IServ", known)
                    self.assertEqual(known, {"/IServ"})
                else:
                    with self.assertRaises(sync_job.RemarqueeCommandError) as failure:
                        sync_job.ensure_remote_directory("/IServ", known)
                    self.assertEqual(failure.exception.diagnostic_code, "RM-TARGET-CREATE-CONFLICT")
                    self.assertFalse(known)
            self.assertEqual(command.call_count, 3)

    def test_file_at_directory_path_is_never_used_as_folder(self):
        file = subprocess.CompletedProcess([], 0, stdout="is_dir=false\n", stderr="")
        with mock.patch.object(sync_job, "run_remarquee", return_value=file) as command:
            with self.assertRaises(sync_job.RemarqueeCommandError) as failure:
                sync_job.ensure_remote_directory("/IServ", set())
        self.assertEqual(failure.exception.diagnostic_code, "RM-TARGET-NOT-DIRECTORY")
        self.assertEqual(command.call_count, 1)

    def test_versioned_copy_is_content_addressed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "blatt.PDF"
            work = root / "work"
            work.mkdir()
            source.write_bytes(b"content")
            first = sync_job.versioned_copy(source, work, "a" * 64)
            second = sync_job.versioned_copy(source, work, "b" * 64)
            self.assertNotEqual(first.name, second.name)
            self.assertIn("aaaaaaaa", first.name)
            self.assertIn("bbbbbbbb", second.name)
            self.assertEqual(first.suffix, ".pdf")


    def test_rmapi_secret_change_replaces_persistent_config_once(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            config_path = root / "rmapi.conf"
            marker_path = root / "rmapi.conf.seed.sha256"
            seed_a = b"devicetoken: device-a\nusertoken: user-a\n"
            seed_b = b"devicetoken: device-b\nusertoken: user-b\n"

            with mock.patch.multiple(
                sync_job,
                RMAPI_CONFIG_PATH=config_path,
                RMAPI_CONFIG_SEED_PATH=marker_path,
            ), mock.patch.dict(
                sync_job.os.environ,
                {"RMAPI_CONFIG_B64": base64.b64encode(seed_a).decode("ascii"), "RMAPI_CONFIG_REPLACE": "false"},
                clear=False,
            ):
                sync_job.install_rmapi_config()
                self.assertEqual(config_path.read_bytes(), seed_a)
                self.assertTrue(marker_path.exists())

                # Simulate a token refresh persisted by rmapi/remarquee. The same
                # Fly secret must not overwrite that newer persistent state.
                refreshed = b"devicetoken: device-a\nusertoken: refreshed-user-a\n"
                config_path.write_bytes(refreshed)
                sync_job.install_rmapi_config()
                self.assertEqual(config_path.read_bytes(), refreshed)

                sync_job.os.environ["RMAPI_CONFIG_B64"] = base64.b64encode(seed_b).decode("ascii")
                sync_job.install_rmapi_config()
                self.assertEqual(config_path.read_bytes(), seed_b)


if __name__ == "__main__":
    unittest.main()

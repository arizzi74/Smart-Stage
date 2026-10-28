#!/usr/bin/env python3
"""Regression checks for authenticated smoke shutdown and strict cleanup."""
from http.server import BaseHTTPRequestHandler, HTTPServer
import json
from pathlib import Path
import subprocess
import tempfile
import threading
import unittest
from unittest.mock import Mock, patch

import smoke_shutdown


class SmokeShutdownTest(unittest.TestCase):
    def test_quit_uses_real_session_cookie_origin_and_csrf(self):
        calls = []

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_POST(self):
                calls.append(self.path)
                self.server.case.assertEqual(self.headers["Origin"], f"http://127.0.0.1:{self.server.server_port}")
                self.server.case.assertEqual(json.loads(self.rfile.read(int(self.headers["Content-Length"]))), {})
                if self.path == "/api/local-session":
                    self.send_response(200)
                    self.send_header("Set-Cookie", "smartstage_admin_session=fixture-session; HttpOnly; SameSite=Strict")
                    result = {"role": "admin", "csrfToken": "fixture-csrf"}
                else:
                    self.server.case.assertEqual(self.path, "/api/quit")
                    self.server.case.assertEqual(self.headers["Cookie"], "smartstage_admin_session=fixture-session")
                    self.server.case.assertEqual(self.headers["X-CSRF-Token"], "fixture-csrf")
                    self.send_response(202)
                    result = {"quitting": True}
                self.end_headers()
                self.wfile.write(json.dumps(result).encode())

        server = HTTPServer(("127.0.0.1", 0), Handler)
        server.case = self
        thread = threading.Thread(target=server.serve_forever)
        thread.start()
        try:
            self.assertTrue(smoke_shutdown.request_admin_quit(server.server_port))
            self.assertEqual(calls, ["/api/local-session", "/api/quit"])
        finally:
            server.shutdown()
            thread.join()
            server.server_close()

    def test_acknowledged_quit_waits_without_forcing(self):
        process = Mock()
        process.poll.return_value = None
        process.wait.return_value = 0
        with patch.object(smoke_shutdown, "request_admin_quit", return_value=True):
            self.assertTrue(smoke_shutdown.stop_application(process, 1234))
        process.terminate.assert_not_called()
        process.send_signal.assert_not_called()
        process.kill.assert_not_called()

    def test_legacy_or_hung_application_keeps_bounded_force_fallback(self):
        process = Mock()
        process.poll.return_value = None
        process.wait.side_effect = [subprocess.TimeoutExpired("fixture", 15), 0]
        with patch.object(smoke_shutdown, "request_admin_quit", return_value=False):
            self.assertFalse(smoke_shutdown.stop_application(process, 1234))
        self.assertEqual(process.terminate.call_count + process.send_signal.call_count, 1)
        process.kill.assert_called_once()
        self.assertEqual(process.wait.call_args_list[-1].kwargs, {"timeout": 5})

    def test_transient_directory_handles_retry_and_persistent_locks_fail(self):
        with tempfile.TemporaryDirectory() as temporary:
            owned = Path(temporary) / "config"
            owned.mkdir()
            remove = smoke_shutdown.shutil.rmtree
            attempts = []

            def delayed(path):
                attempts.append(path)
                if len(attempts) < 3:
                    raise PermissionError("fixture directory still held by guardian")
                remove(path)

            with patch.object(smoke_shutdown.shutil, "rmtree", side_effect=delayed), patch.object(smoke_shutdown.time, "sleep"):
                smoke_shutdown.remove_test_directory(owned)
            self.assertEqual(len(attempts), 3)
            self.assertFalse(owned.exists())
            owned.mkdir()
            with patch.object(smoke_shutdown.shutil, "rmtree", side_effect=PermissionError("fixture persistent lock")):
                with self.assertRaises(AssertionError):
                    smoke_shutdown.remove_test_directory(owned, timeout=0)
            self.assertTrue(owned.exists())


if __name__ == "__main__":
    unittest.main()

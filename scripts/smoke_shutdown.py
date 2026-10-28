"""Stop test-owned app processes and require complete temporary cleanup."""
import http.client
from http.cookies import SimpleCookie
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import time


def request_admin_quit(port):
    """Use the real loopback session, cookie, Origin and CSRF-protected Quit."""
    if not port:
        return False
    origin = f"http://127.0.0.1:{port}"

    def post(path, headers):
        connection = http.client.HTTPConnection("127.0.0.1", port, timeout=2)
        try:
            connection.request("POST", path, body=b"{}", headers={
                "Origin": origin, "Content-Type": "application/json", **headers,
            })
            response = connection.getresponse()
            return response.status, response.getheaders(), json.loads(response.read())
        finally:
            connection.close()

    try:
        status, headers, session = post("/api/local-session", {})
        if status != 200 or session.get("role") != "admin" or not session.get("csrfToken"):
            return False
        cookies = SimpleCookie()
        for name, value in headers:
            if name.lower() == "set-cookie":
                cookies.load(value)
        if "smartstage_admin_session" not in cookies:
            return False
        cookie = cookies["smartstage_admin_session"]
        status, _, result = post("/api/quit", {
            "Cookie": f"{cookie.key}={cookie.value}", "X-CSRF-Token": session["csrfToken"],
        })
        return status == 202 and result.get("quitting") is True
    except (OSError, http.client.HTTPException, ValueError, AttributeError):
        # Older releases lack Quit/local-session; failed startup may also leave
        # no usable API. The caller still stops and reaps its own process.
        return False


def stop_application(process, admin_port=None):
    """Try authenticated Quit, then bound fallback termination and process wait."""
    if process is None or process.poll() is not None:
        return False
    if request_admin_quit(admin_port):
        try:
            code = process.wait(timeout=15)
        except subprocess.TimeoutExpired:
            pass
        else:
            assert code == 0, "Authenticated Admin Quit did not exit the test app cleanly"
            return True
    try:
        if process.poll() is None:
            if os.name == "nt":
                process.terminate()
            else:
                process.send_signal(signal.SIGINT)
    except ProcessLookupError:
        pass
    try:
        process.wait(timeout=15)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)
    return False


def remove_test_directory(directory, timeout=15):
    """Wait for guardian/WebView handles, requiring actual complete removal."""
    path = Path(directory)
    deadline = time.monotonic() + timeout
    while True:
        try:
            shutil.rmtree(path)
        except FileNotFoundError:
            if not path.exists():
                return
        except OSError as error:
            if time.monotonic() >= deadline:
                raise AssertionError("Test directory remains in use after native process exit") from error
        else:
            assert not path.exists(), "Test directory cleanup did not remove the owned directory"
            return
        if time.monotonic() >= deadline:
            raise AssertionError("Test directory cleanup did not finish after native process exit")
        time.sleep(0.1)

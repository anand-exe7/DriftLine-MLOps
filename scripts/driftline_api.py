"""Tiny stdlib-only client for the Driftline control-plane API (used by the scripts)."""
import json
import os
import uuid
import urllib.error
import urllib.request

API = os.getenv("DRIFTLINE_API", "http://localhost:8080/api/v1")
API_KEY = os.getenv("DRIFTLINE_API_KEY", "")


class ApiError(Exception):
    def __init__(self, status, message):
        super().__init__(f"{status}: {message}")
        self.status = status


def _request(method, path, body=None, headers=None, timeout=60):
    headers = dict(headers or {})
    if API_KEY:
        headers["X-API-Key"] = API_KEY
    req = urllib.request.Request(API + path, data=body, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read()
            return json.loads(raw) if raw else None
    except urllib.error.HTTPError as err:
        raw = err.read().decode("utf-8", "replace")
        try:
            raw = json.loads(raw).get("error", raw)
        except ValueError:
            pass
        raise ApiError(err.code, raw) from None


def get(path):
    return _request("GET", path)


def post(path, data=None):
    body = json.dumps(data or {}).encode()
    return _request("POST", path, body, {"content-type": "application/json"})


def post_multipart(path, fields, files):
    """fields: {name: str}; files: {name: (filename, bytes)}"""
    boundary = uuid.uuid4().hex
    parts = []
    for name, value in fields.items():
        parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{name}"\r\n\r\n{value}\r\n'.encode())
    for name, (filename, content) in files.items():
        parts.append(
            f'--{boundary}\r\nContent-Disposition: form-data; name="{name}"; filename="{filename}"\r\n'
            f"Content-Type: application/octet-stream\r\n\r\n".encode() + content + b"\r\n"
        )
    parts.append(f"--{boundary}--\r\n".encode())
    return _request("POST", path, b"".join(parts), {"content-type": f"multipart/form-data; boundary={boundary}"})

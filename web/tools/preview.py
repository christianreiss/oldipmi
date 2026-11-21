#!/usr/bin/env python3
"""Serves web/site on http://127.0.0.1:8765/ with the headers of the real server.

Usage: web/tools/preview.py [PORT]

The production web server must send the same headers (HEADERS below), answer
unknown paths and directories with 404.html and status 404, and list no
directories. A page that works here therefore works there, and anything the
Content-Security-Policy refuses shows up in the browser console.
"""
import functools
import http.server
import os
import sys

HEADERS = {
    "Content-Security-Policy": "default-src 'none'; img-src 'self'; style-src 'self'; "
    "font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
    "X-Content-Type-Options": "nosniff",
    "Referrer-Policy": "no-referrer",
}
SITE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "site")


class Handler(http.server.SimpleHTTPRequestHandler):
    def end_headers(self):
        for name, value in HEADERS.items():
            self.send_header(name, value)
        super().end_headers()

    def send_error(self, code, message=None, explain=None):
        if code != 404:
            super().send_error(code, message, explain)
            return
        with open(os.path.join(self.directory, "404.html"), "rb") as page:
            body = page.read()
        self.send_response(404)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)

    def list_directory(self, path):
        self.send_error(404)


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 8765
    handler = functools.partial(Handler, directory=os.path.normpath(SITE))
    print(f"http://127.0.0.1:{port}/", flush=True)
    http.server.ThreadingHTTPServer(("127.0.0.1", port), handler).serve_forever()

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
FAKE_COMMAND = r'''
import json
import os
from pathlib import Path
import sys

command = Path(sys.argv[0]).name
args = sys.argv[1:]
state = Path(os.environ["FAKE_STATE"])
with state.with_suffix(".jsonl").open("a") as log:
    log.write(json.dumps([command, args, os.getcwd()]) + "\n")
if command == "docker":
    sys.exit("Docker must not be used")
elif command == "podman-compose":
    if "build" in args:
        sys.exit(int(os.environ.get("BUILD_EXIT", "0")))
    if "up" in args:
        state.touch()
elif command == "podman":
    if args[0] == "ps":
        if state.exists() or os.environ.get("EXISTING"):
            print("test-container")
    elif args[0] == "inspect":
        print("localhost/oldipmi-kvm" if "ImageName" in args[2] else "sha256:old")
    elif args[:2] == ["image", "inspect"]:
        print("sha256:new" if os.environ.get("IMAGE_CHANGED") else "sha256:old")
    elif args[0] == "port":
        print(os.environ.get("BINDING", "127.0.0.1:8088"))
elif command == "curl":
    print(os.environ.get("HTTP_STATUS", "200"))
'''


class StartTests(unittest.TestCase):
    def run_start(self, *args, **overrides):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            for name in ("podman", "podman-compose", "docker", "curl", "xdg-open"):
                command = directory / name
                command.write_text(f"#!{sys.executable}\n" + FAKE_COMMAND)
                command.chmod(0o755)
            state = directory / "state"
            env = dict(os.environ, PATH=f"{directory}:{os.defpath}",
                       FAKE_STATE=str(state), DISPLAY=":99", **overrides)
            result = subprocess.run(
                [str(ROOT / "start.sh"), *args], cwd=directory, env=env,
                capture_output=True, text=True, timeout=10,
            )
            calls = [json.loads(line) for line in state.with_suffix(".jsonl").read_text().splitlines()]
        return result, calls

    def test_headless_launch_from_another_directory(self):
        result, calls = self.run_start("--no-browser", "another-server")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("http://127.0.0.1:8088/", result.stdout)
        self.assertTrue(all(cwd == str(ROOT) for _, _, cwd in calls))
        compose = [args for cmd, args, _ in calls if cmd == "podman-compose"]
        self.assertEqual(compose, [
            ["-f", str(ROOT / "docker-compose.yml"), "build", "another-server"],
            ["-f", str(ROOT / "docker-compose.yml"), "up", "--detach", "--no-build", "another-server"],
        ])
        self.assertFalse(any(cmd in ("docker", "xdg-open") for cmd, _, _ in calls))

    def test_unchanged_image_preserves_session(self):
        result, calls = self.run_start("--no-browser", EXISTING="1")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(any("--force-recreate" in args for _, args, _ in calls))

    def test_rebuilt_image_recreates_container(self):
        result, calls = self.run_start("--no-browser", EXISTING="1", IMAGE_CHANGED="1")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(any("--force-recreate" in args for _, args, _ in calls))

    def test_failed_build_stops_before_starting_or_opening_browser(self):
        result, calls = self.run_start(BUILD_EXIT="17")
        self.assertEqual(result.returncode, 17)
        self.assertFalse(any(cmd in ("curl", "xdg-open") or "up" in args
                             for cmd, args, _ in calls))

    def test_auth_protected_page_is_ready_and_opens_browser(self):
        result, calls = self.run_start(HTTP_STATUS="401")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(["xdg-open", ["http://127.0.0.1:8088/"], str(ROOT)], calls)

    def test_wildcard_bindings_use_loopback_urls(self):
        for binding, url in (("0.0.0.0:8088", "http://127.0.0.1:8088/"),
                             ("[::]:8088", "http://[::1]:8088/")):
            with self.subTest(binding=binding):
                result, _ = self.run_start("--no-browser", BINDING=binding)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn(url, result.stdout)

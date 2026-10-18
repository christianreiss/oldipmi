import importlib.machinery
import io
from pathlib import Path
import tempfile
import types
import unittest
from unittest.mock import Mock, patch
import zipfile


loader = importlib.machinery.SourceFileLoader(
    "launcher", str(Path(__file__).resolve().parents[1] / "bin/jviewer-launch")
)
launcher = types.ModuleType(loader.name)
loader.exec_module(launcher)


def jar_bytes():
    stream = io.BytesIO()
    with zipfile.ZipFile(stream, "w") as archive:
        archive.writestr("META-INF/MANIFEST.MF", "Manifest-Version: 1.0\n")
    return stream.getvalue()


class LauncherTests(unittest.TestCase):
    def test_legacy_https_download_disables_certificate_verification(self):
        session = launcher.bmc_session()
        response = launcher.requests.Response()
        response.status_code = 200
        response._content = jar_bytes()
        with tempfile.TemporaryDirectory() as directory, \
                patch.object(session.get_adapter("https://bmc/"), "send",
                             return_value=response) as send:
            dest = launcher.download(session, "https://bmc/", "viewer.jar", directory)
            self.assertTrue(zipfile.is_zipfile(dest))
        self.assertIs(send.call_args.kwargs["verify"], False)

    def test_truncated_https_jnlp_uses_unverified_tls(self):
        session = launcher.bmc_session()
        with patch.object(launcher.http.client, "HTTPSConnection") as connection:
            response = connection.return_value.getresponse.return_value
            response.status = 200
            response.read.side_effect = launcher.http.client.IncompleteRead(b"<jnlp/>", 10)
            result = launcher.tolerant_get(session, "https://bmc:8443/viewer.jnlp")
        self.assertEqual(result, (200, b"<jnlp/>", True))
        self.assertEqual(connection.call_args.args, ("bmc", 8443))
        context = connection.call_args.kwargs["context"]
        self.assertFalse(context.check_hostname)
        self.assertEqual(context.verify_mode, launcher.ssl.CERT_NONE)
        connection.return_value.close.assert_called_once_with()

    def test_aten_metadata_and_platform_selection(self):
        result = launcher.parse_jnlp('''<jnlp codebase="http://bmc/">
          <resources><jar href="viewer.jar"/>
            <property name="jnlp.packEnabled" value="true"/></resources>
          <resources os="Windows"><nativelib href="windows.jar"/></resources>
          <resources os="Linux" arch="x86"><nativelib href="32bit.jar"/></resources>
          <resources os="Linux" arch="amd64"><nativelib href="native.jar"/></resources>
          <resources os="Linux" arch="x86_64"><nativelib href="native.jar"/></resources>
          <application-desc main-class="tw.com.aten.ikvm.KVMMain">
            <argument>host</argument><argument>secret</argument></application-desc>
        </jnlp>''')
        self.assertEqual(result, ("http://bmc/", ["viewer.jar"], ["native.jar"],
                                 ["host", "secret"], "tw.com.aten.ikvm.KVMMain", True))

    def test_ami_defaults_unchanged(self):
        result = launcher.parse_jnlp('''<jnlp codebase="http://bmc/Java">
          <resources><jar href="JViewer.jar"/></resources>
          <application-desc><argument>host</argument></application-desc></jnlp>''')
        self.assertEqual(result[-2:], (launcher.MAIN_CLASS, False))
        self.assertEqual(result[0], "http://bmc/Java/")

    def test_plain_jar(self):
        session = Mock()
        session.get.return_value = Mock(status_code=200, content=jar_bytes())
        with tempfile.TemporaryDirectory() as directory:
            dest = launcher.download(session, "http://bmc/", "viewer.jar", directory)
            self.assertTrue(zipfile.is_zipfile(dest))
        session.get.assert_called_once_with("http://bmc/viewer.jar", timeout=60)

    def test_packed_jar_is_unpacked_and_temporary_file_removed(self):
        session = Mock()
        session.get.return_value = Mock(status_code=200, content=b"packed")
        def unpack(cmd, **kwargs):
            self.assertEqual(Path(cmd[1]).read_bytes(), b"packed")
            Path(cmd[2]).write_bytes(jar_bytes())
        with tempfile.TemporaryDirectory() as directory, \
                patch.object(launcher.shutil, "which", return_value="/java/unpack200"), \
                patch.object(launcher.subprocess, "run", side_effect=unpack):
            dest = launcher.download(session, "http://bmc/", "viewer.jar?v=1", directory, True)
            self.assertTrue(zipfile.is_zipfile(dest))
            self.assertFalse(Path(dest + ".pack.gz").exists())
        session.get.assert_called_once_with("http://bmc/viewer.jar.pack.gz?v=1", timeout=60)

    def test_missing_pack_falls_back_to_plain(self):
        session = Mock()
        session.get.side_effect = [Mock(status_code=404), Mock(status_code=200, content=jar_bytes())]
        with tempfile.TemporaryDirectory() as directory:
            launcher.download(session, "http://bmc/", "viewer.jar", directory, True)
        self.assertEqual([call.args[0] for call in session.get.call_args_list],
                         ["http://bmc/viewer.jar.pack.gz", "http://bmc/viewer.jar"])

    def test_auth_failure_does_not_fall_back(self):
        session = Mock()
        session.get.return_value = Mock(status_code=403)
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(SystemExit, "HTTP 403"):
                launcher.download(session, "http://bmc/", "viewer.jar", directory, True)
        self.assertEqual(session.get.call_count, 1)

    def test_html_response_is_rejected(self):
        session = Mock()
        session.get.return_value = Mock(status_code=200, content=b"<html>login required</html>")
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(SystemExit, "not a valid JAR"):
                launcher.download(session, "http://bmc/", "viewer.jar", directory)
            self.assertEqual(list(Path(directory).iterdir()), [])


if __name__ == "__main__":
    unittest.main()

// The error page's hints (desktop/qml/families.js): which next step each kind of
// backend failure gets. The messages are shaped like those oldipmi-core sends.
#include <QFile>
#include <QJSEngine>
#include <QtTest>

class FamiliesTest : public QObject {
  Q_OBJECT
  QJSEngine m_engine;
  QString call(const char *function, const QString &message) {
    QJSValue result = m_engine.globalObject().property(function).call({message});
    return result.isError() ? "error: " + result.toString() : result.toString();
  }
private slots:
  void initTestCase() {
    QFile f(FAMILIES_JS);
    QVERIFY2(f.open(QIODevice::ReadOnly), FAMILIES_JS);
    // A QML JavaScript library; without its pragma it is plain JavaScript.
    QString source = QString::fromUtf8(f.readAll()).replace(".pragma library", "");
    QJSValue loaded = m_engine.evaluate(source, FAMILIES_JS);
    QVERIFY2(!loaded.isError(), qPrintable(loaded.toString()));
  }
  void errorHint_data() {
    QTest::addColumn<QString>("message");
    QTest::addColumn<QString>("hint"); // the start of the expected hint, "" for none
    QTest::newRow("dns") << "Login failed: the BMC web login (/cgi/login.cgi) failed: BMC HTTP transport "
                            "failed: Post \"https://bmc.example.invalid/cgi/login.cgi\": dial tcp: lookup "
                            "bmc.example.invalid on 192.0.2.53:53: no such host"
                         << "Check the BMC address";
    QTest::newRow("unreachable demo") << "Login failed: demo BMC unreachable (the host name contains "
                                         "\"unreachable\"): no route to host"
                                      << "The BMC did not answer";
    QTest::newRow("timeout") << "Login failed: BMC HTTP transport failed: Post \"https://192.0.2.10/rpc/"
                                "WEBSES/create.asp\": dial tcp 192.0.2.10:443: i/o timeout"
                             << "The BMC did not answer";
    QTest::newRow("refused") << "Console connection failed: dial tcp 192.0.2.10:5900: connect: connection refused"
                             << "The BMC refused the connection";
    // Go wraps a name mismatch in "failed to verify certificate": the name rule wins.
    QTest::newRow("name mismatch") << "Login failed: tls: failed to verify certificate: x509: certificate is "
                                      "valid for bmc.local, not bmc.example.net"
                                   << "The BMC's certificate does not name this address";
    QTest::newRow("unknown authority") << "Login failed: tls: failed to verify certificate: x509: certificate "
                                          "signed by unknown authority"
                                       << "The BMC's certificate cannot be verified";
    QTest::newRow("tls 1.0") << "Login failed: tls: server selected unsupported protocol version 301"
                             << "The BMC uses an old TLS version";
    QTest::newRow("tls 1.3") << "Login failed: tls: server selected unsupported protocol version 304"
                             << "The secure connection (TLS) to the BMC failed";
    QTest::newRow("handshake") << "Console connection failed: remote error: tls: handshake failure"
                               << "The secure connection (TLS) to the BMC failed";
    QTest::newRow("rejected") << "Login failed: BMC rejected login (HAPI_STATUS=1)"
                              << "Check the username and password";
    QTest::newRow("text mode") << "Console connection failed: unsupported video format: this BMC is in "
                                  "text mode (Cisco IMC RLE text), which OldIPMI cannot draw yet"
                               << "The server's screen is in text mode";
    QTest::newRow("plain console") << "Console connection failed: BMC requests an unencrypted console; "
                                      "enable this host's plain-console option explicitly"
                                   << "This BMC offers only an unencrypted console";
    QTest::newRow("no hint") << "Console discovery failed: the BMC sent an unexpected reply" << "";
  }
  void errorHint() {
    QFETCH(QString, message);
    QFETCH(QString, hint);
    const QString got = call("errorHint", message);
    if (hint.isEmpty())
      QCOMPARE(got, QString());
    else
      QVERIFY2(got.startsWith(hint), qPrintable(got));
  }
  void errorDetailDropsTheStage() {
    QCOMPARE(call("errorDetail", "Login failed: dial tcp: lookup x: no such host"),
             QString("Dial tcp: lookup x: no such host"));
    QCOMPARE(call("errorDetail", "Backend crashed"), QString("Backend crashed"));
  }
};
QTEST_GUILESS_MAIN(FamiliesTest)
#include "families_test.moc"

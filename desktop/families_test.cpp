// The error page's hints (desktop/qml/families.js): which next step each kind of
// backend failure gets. The messages are shaped like those oldipmi-core sends.
// And the family helpers there, which answer from the family table.
#include "families.h"
#include <QFile>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QSet>
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
    // A QML JavaScript library; without its pragma and import it is plain
    // JavaScript, given the family table the way the app registers it.
    QString source = QString::fromUtf8(f.readAll()).replace(".pragma library", "").replace(".import OldIPMI 1.0 as App", "");
    QJSValue app = m_engine.newObject();
    app.setProperty("FamilyTable", familyTableScript(&m_engine));
    m_engine.globalObject().setProperty("App", app);
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
  // testdata/ipc/errors.json: errors as the core words them, with the class it
  // sends as their code (internal/protocol and the drivers' tests check the
  // rows). Each code stands for one of the hints, and each message gets the
  // hint of its code: the code says what the text says. Except where only the
  // code knows yet that the BMC rejected the login.
  void errorCodesMatchTheHints() {
    const QMap<QString, QString> hintOf{{"dns", "Check the BMC address"},
                                        {"unreachable", "The BMC did not answer"},
                                        {"refused", "The BMC refused the connection"},
                                        {"tls_name", "The BMC's certificate does not name this address"},
                                        {"tls_unverified", "The BMC's certificate cannot be verified"},
                                        {"tls_old", "The BMC uses an old TLS version"},
                                        {"tls", "The secure connection (TLS) to the BMC failed"},
                                        {"auth", "Check the username and password"},
                                        {"text_mode", "The server's screen is in text mode"},
                                        {"plain_console", "This BMC offers only an unencrypted console"}};
    const QStringList codeOnly{"aten: no web session", "aten: Redfish login rejected",
                               "avocent: invalid user name", "avocent: invalid password"};
    QFile names(IPC_FIXTURES "/names.json"), errors(IPC_FIXTURES "/errors.json");
    QVERIFY(names.open(QIODevice::ReadOnly));
    QVERIFY(errors.open(QIODevice::ReadOnly));
    QStringList codes;
    for (const QJsonValue &code : QJsonDocument::fromJson(names.readAll())["codes"].toObject())
      codes << code.toString();
    codes.sort();
    QCOMPARE(codes, hintOf.keys());
    QSet<QString> seen, exceptions;
    const QJsonArray rows = QJsonDocument::fromJson(errors.readAll())["errors"].toArray();
    QVERIFY(!rows.isEmpty());
    for (const QJsonValue &v : rows) {
      const QString name = v["name"].toString(), code = v["code"].toString();
      const QString hint = call("errorHint", v["message"].toString());
      QVERIFY2(code.isEmpty() || hintOf.contains(code), qPrintable(name + ": unknown code " + code));
      seen << code;
      if (codeOnly.contains(name)) {
        exceptions << name;
        QVERIFY2(code == "auth" && hint.isEmpty(), qPrintable(name + ": " + code + ", " + hint));
      } else if (code.isEmpty()) {
        QVERIFY2(hint.isEmpty(), qPrintable(name + ": " + hint));
      } else {
        QVERIFY2(hint.startsWith(hintOf[code]), qPrintable(name + ": " + code + ", " + hint));
      }
    }
    // An example of each class, of errors of none, and of each exception.
    for (const QString &code : hintOf.keys())
      QVERIFY2(seen.contains(code), qPrintable("errors.json has no example of " + code));
    QVERIFY(seen.contains(""));
    QCOMPARE(exceptions.size(), codeOnly.size());
  }
  // The family helpers answer from the table (desktop/families.json), which QML
  // gets as a JavaScript array; the controller asks the same table.
  void familyHelpers() {
    QVERIFY(m_engine.evaluate("Array.isArray(list)").toBool());
    const QJsonArray table = familyTable();
    QVERIFY(!table.isEmpty());
    QCOMPARE(m_engine.evaluate("list.length").toInt(), table.size());
    for (int i = 0; i < table.size(); ++i) {
      const QJsonObject f = table[i].toObject();
      const QString id = f["id"].toString();
      auto js = [&](const char *expression) { return m_engine.evaluate(QString(expression).replace("ID", "'" + id + "'")); };
      QCOMPARE(js("index(ID)").toInt(), i);
      QCOMPARE(js("name(ID)").toString(), f["name"].toString());
      QCOMPARE(js("shortName(ID)").toString(), f["short"].toString());
      QCOMPARE(js("image(ID)").toString(), "qrc:/images/family-" + id + ".png");
      QCOMPARE(js("offers(ID, 'consoleMode')").toBool(), f["consoleMode"].toBool());
      QCOMPARE(js("offers(ID, 'jnlp')").toBool(), f["jnlp"].toBool());
      QCOMPARE(js("offers(ID, 'clientCertificate')").toBool(), f["clientCertificate"].toBool());
      QCOMPARE(js("needsAccount(ID)").toBool(), f["account"].toBool());
      QCOMPARE(familyNeedsAccount(id), f["account"].toBool());
    }
    // An unknown family has no place in the list, keeps its id as its names,
    // offers no option and needs an account.
    QCOMPARE(m_engine.evaluate("index('fixture')").toInt(), -1);
    QCOMPARE(m_engine.evaluate("name('fixture')").toString(), QString("fixture"));
    QCOMPARE(m_engine.evaluate("shortName('fixture')").toString(), QString("fixture"));
    QCOMPARE(m_engine.evaluate("offers('fixture', 'jnlp')").toBool(), false);
    QCOMPARE(m_engine.evaluate("needsAccount('fixture')").toBool(), true);
    QVERIFY(familyNeedsAccount("fixture"));
  }
  void errorDetailDropsTheStage() {
    QCOMPARE(call("errorDetail", "Login failed: dial tcp: lookup x: no such host"),
             QString("Dial tcp: lookup x: no such host"));
    QCOMPARE(call("errorDetail", "Backend crashed"), QString("Backend crashed"));
  }
};
QTEST_GUILESS_MAIN(FamiliesTest)
#include "families_test.moc"

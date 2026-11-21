// testdata/ipc/names.json is the one list of the IPC's names and its frame
// limit. Here the constants of desktop/ipc.h are checked against it in both
// directions: every constant is in the file with its value, and every entry of
// the file is a constant. internal/backend/names_test.go does the same for the
// core.
#include "ipc.h"
#include <QFile>
#include <QJsonDocument>
#include <QJsonObject>
#include <QRegularExpression>
#include <QTest>

class IpcNames : public QObject {
  Q_OBJECT
private slots:
  void headerMatchesNamesJson();
};

static QStringList pairs(const QVariantMap &m) {
  QStringList out;
  for (auto it = m.cbegin(); it != m.cend(); ++it)
    out << it.key() + "=" + it.value().toString();
  return out;
}

void IpcNames::headerMatchesNamesJson() {
  QFile names(IPC_FIXTURES "/names.json"), header(IPC_HEADER);
  QVERIFY(names.open(QIODevice::ReadOnly));
  QVERIFY(header.open(QIODevice::ReadOnly));
  const QJsonObject fixture = QJsonDocument::fromJson(names.readAll()).object();
  // The constants of ipc.h by group, read from its text.
  static const QRegularExpression constant(
      R"re(^inline constexpr char (Op|Event|State|Stage|Power|Code)(\w+)\[\] = "([^"]*)";)re");
  static const QRegularExpression limit(R"re(^inline constexpr unsigned (MaxFrame)(\w+) = (\d+);)re");
  QMap<QString, QVariantMap> got;
  for (const QString &line : QString::fromUtf8(header.readAll()).split('\n')) {
    if (!line.contains("constexpr"))
      continue;
    auto m = constant.match(line);
    if (!m.hasMatch())
      m = limit.match(line);
    QVERIFY2(m.hasMatch(), qPrintable("ipc.h: not one constant in the usual form: " + line));
    got[m.captured(1)][m.captured(2)] = m.captured(3);
  }
  const QMap<QString, QString> groups{{"Op", "ops"},       {"Event", "events"},
                                      {"State", "states"}, {"Stage", "stages"},
                                      {"Power", "powerActions"}, {"Code", "codes"},
                                      {"MaxFrame", "maxFrame"}};
  for (auto it = groups.cbegin(); it != groups.cend(); ++it) {
    const QVariantMap want = fixture.value(it.value()).toObject().toVariantMap();
    QVERIFY2(!want.isEmpty(), qPrintable("names.json has no " + it.value()));
    const QStringList have = pairs(got.value(it.key())), expected = pairs(want);
    QVERIFY2(have == expected, qPrintable(QString("ipc::%1* are %2; names.json has %3")
                                              .arg(it.key(), have.join(' '), expected.join(' '))));
  }
  // The text is what the desktop compiles.
  QCOMPARE(QString(ipc::StageLoggingIn), fixture["stages"]["LoggingIn"].toString());
  QCOMPARE(ipc::MaxFrameWidth, unsigned(fixture["maxFrame"]["Width"].toInt()));
  QCOMPARE(ipc::MaxFrameHeight, unsigned(fixture["maxFrame"]["Height"].toInt()));
}

QTEST_GUILESS_MAIN(IpcNames)
#include "ipc_test.moc"

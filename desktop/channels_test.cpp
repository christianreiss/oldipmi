// The framing of the control and video sockets (channels.h), without sockets.
#include "channels.h"
#include "ipc.h"
#include <QJsonDocument>
#include <QtEndian>
#include <QtTest>

// A message as the core frames it.
static QByteArray framed(const QByteArray &json) {
  QByteArray h(4, 0);
  qToBigEndian<quint32>(quint32(json.size()), h.data());
  return h + json;
}

// A picture as the core sends it; pixel i has red i, green 2i, blue 3i.
static QByteArray picture(quint32 w, quint32 h) {
  QByteArray b(12, 0);
  qToBigEndian<quint32>(8 + w * h * 4, b.data());
  qToBigEndian<quint32>(w, b.data() + 4);
  qToBigEndian<quint32>(h, b.data() + 8);
  for (quint32 i = 0; i < w * h; ++i) {
    const char pixel[4] = {char(i), char(2 * i), char(3 * i), char(255)};
    b.append(pixel, 4);
  }
  return b;
}

// The header of a picture, with a length that need not fit it.
static QByteArray header(quint32 n, quint32 w, quint32 h) {
  QByteArray b(12, 0);
  qToBigEndian<quint32>(n, b.data());
  qToBigEndian<quint32>(w, b.data() + 4);
  qToBigEndian<quint32>(h, b.data() + 8);
  return b;
}

class ChannelsTest : public QObject {
  Q_OBJECT
private slots:
  void commandIsFramed() {
    const QByteArray b = ControlChannel::frame({{"op", "key"}, {"id", 7}});
    QCOMPARE(qFromBigEndian<quint32>(b.constData()), quint32(b.size() - 4));
    QCOMPARE(QJsonDocument::fromJson(b.mid(4)).object(), (QJsonObject{{"op", "key"}, {"id", 7}}));
  }

  void eventCarriesWhatTheMessageHas() {
    ControlChannel c;
    c.append(framed(R"({"version":1,"type":"state","state":"connected","info":{"model":"X"},"mounted":true})"));
    ControlEvent e;
    QCOMPARE(c.next(e), ControlChannel::Status::Event);
    QCOMPARE(e.type, QString(ipc::EventState));
    QCOMPARE(e.id, 0);
    QCOMPARE(e.state, std::optional<QString>(ipc::StateConnected));
    QCOMPARE(e.info, std::optional<QVariantMap>(QVariantMap{{"model", "X"}}));
    QCOMPARE(e.mounted, std::optional<bool>(true));
    QCOMPARE(c.next(e), ControlChannel::Status::NeedMore);
  }

  void eventLeavesOutWhatTheMessageLacks() {
    ControlChannel c;
    c.append(framed(R"({"version":1,"type":"error","id":4,"message":"no","code":"auth"})"));
    ControlEvent e;
    e.state = "stale"; // a new event starts empty
    QCOMPARE(c.next(e), ControlChannel::Status::Event);
    QCOMPARE(e.type, QString(ipc::EventError));
    QCOMPARE(e.id, 4);
    QCOMPARE(e.message, QString("no"));
    QCOMPARE(e.code, QString(ipc::CodeAuth));
    QVERIFY(!e.state && !e.info && !e.mounted);
  }

  void messagesArriveInAnyPieces() {
    const QByteArray both = framed(R"({"version":1,"type":"ok","id":1})") + framed(R"({"version":1,"type":"ok","id":2})");
    ControlChannel c;
    QList<int> ids;
    ControlEvent e;
    for (char byte : both) { // one byte at a time
      c.append(QByteArray(1, byte));
      while (c.next(e) == ControlChannel::Status::Event)
        ids << e.id;
    }
    QCOMPARE(ids, (QList<int>{1, 2}));
    c.append(both); // and both at once
    QCOMPARE(c.next(e), ControlChannel::Status::Event);
    QCOMPARE(e.id, 1);
    QCOMPARE(c.next(e), ControlChannel::Status::Event);
    QCOMPARE(e.id, 2);
    QCOMPARE(c.next(e), ControlChannel::Status::NeedMore);
  }

  void badLengthRepeats_data() {
    QTest::addColumn<quint32>("length");
    QTest::newRow("zero") << quint32(0);
    QTest::newRow("over 1 MiB") << ControlChannel::MaxMessage + 1;
  }
  void badLengthRepeats() {
    QFETCH(quint32, length);
    QByteArray h(4, 0);
    qToBigEndian<quint32>(length, h.data());
    ControlChannel c;
    c.append(h);
    ControlEvent e;
    QCOMPARE(c.next(e), ControlChannel::Status::BadLength);
    QCOMPARE(c.next(e), ControlChannel::Status::BadLength);
    c.clear();
    QCOMPARE(c.next(e), ControlChannel::Status::NeedMore);
  }

  void longestMessageIsRead() {
    // A message whose text fills it up to the limit exactly.
    QJsonObject o{{"version", 1}, {"type", "ok"}, {"id", 3}, {"message", ""}};
    const auto size = QJsonDocument(o).toJson(QJsonDocument::Compact).size();
    o["message"] = QString(int(ControlChannel::MaxMessage - size), QChar('x'));
    const QByteArray json = QJsonDocument(o).toJson(QJsonDocument::Compact);
    QCOMPARE(quint32(json.size()), ControlChannel::MaxMessage);
    ControlChannel c;
    c.append(framed(json));
    ControlEvent e;
    QCOMPARE(c.next(e), ControlChannel::Status::Event);
    QCOMPARE(e.id, 3);
  }

  // A message of another version is taken out; what follows it still reads.
  void otherVersionIsTakenOut_data() {
    QTest::addColumn<QByteArray>("json");
    QTest::newRow("version 2") << QByteArray(R"({"version":2,"type":"ok","id":1})");
    QTest::newRow("no version") << QByteArray(R"({"type":"ok","id":1})");
  }
  void otherVersionIsTakenOut() {
    QFETCH(QByteArray, json);
    ControlChannel c;
    c.append(framed(json) + framed(R"({"version":1,"type":"ok","id":2})"));
    ControlEvent e;
    QCOMPARE(c.next(e), ControlChannel::Status::BadVersion);
    QCOMPARE(c.next(e), ControlChannel::Status::Event);
    QCOMPARE(e.id, 2);
  }

  // So is a message that is not a JSON object.
  void notAnObjectIsTakenOut_data() {
    QTest::addColumn<QByteArray>("json");
    QTest::newRow("cut off") << QByteArray("{\"version\":1,");
    QTest::newRow("not JSON") << QByteArray("version 1");
    QTest::newRow("array") << QByteArray(R"([{"version":1}])");
    QTest::newRow("string") << QByteArray(R"("version 1")");
  }
  void notAnObjectIsTakenOut() {
    QFETCH(QByteArray, json);
    ControlChannel c;
    c.append(framed(json) + framed(R"({"version":1,"type":"ok","id":2})"));
    ControlEvent e;
    QCOMPARE(c.next(e), ControlChannel::Status::BadMessage);
    QCOMPARE(c.next(e), ControlChannel::Status::Event);
    QCOMPARE(e.id, 2);
  }

  void latestPictureWins() {
    VideoChannel v;
    v.append(picture(2, 1) + picture(3, 2));
    QImage latest;
    QCOMPARE(v.take(latest), VideoChannel::Status::Ok);
    QCOMPARE(latest.size(), QSize(3, 2));
    QCOMPARE(latest.format(), QImage::Format_RGBA8888);
    QCOMPARE(latest.pixelColor(2, 1), QColor(5, 10, 15, 255)); // pixel 5
    QCOMPARE(v.take(latest), VideoChannel::Status::Ok);
    QVERIFY(latest.isNull());
  }

  void pictureArrivesInPieces() {
    const QByteArray b = picture(4, 4);
    VideoChannel v;
    QImage latest;
    v.append(b.left(12 + 30));
    QCOMPARE(v.take(latest), VideoChannel::Status::Ok);
    QVERIFY(latest.isNull());
    v.append(b.mid(12 + 30));
    QCOMPARE(v.take(latest), VideoChannel::Status::Ok);
    QCOMPARE(latest.size(), QSize(4, 4));
  }

  void badHeaderIsRefused_data() {
    QTest::addColumn<QByteArray>("bytes");
    QTest::newRow("no width") << header(8, 0, 1);
    QTest::newRow("no height") << header(8, 1, 0);
    QTest::newRow("too wide") << header(8 + (ipc::MaxFrameWidth + 1) * 4, ipc::MaxFrameWidth + 1, 1);
    QTest::newRow("too high") << header(8 + (ipc::MaxFrameHeight + 1) * 4, 1, ipc::MaxFrameHeight + 1);
    QTest::newRow("length too short") << header(8 + 3, 1, 1);
    QTest::newRow("length too long") << header(8 + 5, 1, 1);
    QTest::newRow("after a good one") << picture(1, 1) + header(8, 0, 1);
  }
  void badHeaderIsRefused() {
    QFETCH(QByteArray, bytes);
    VideoChannel v;
    v.append(bytes);
    QImage latest(1, 1, QImage::Format_RGBA8888);
    QCOMPARE(v.take(latest), VideoChannel::Status::BadFrame);
    QVERIFY(latest.isNull());
  }

  void largestPictureIsRead() {
    VideoChannel v;
    v.append(picture(ipc::MaxFrameWidth, ipc::MaxFrameHeight));
    QImage latest;
    QCOMPARE(v.take(latest), VideoChannel::Status::Ok);
    QCOMPARE(latest.size(), QSize(ipc::MaxFrameWidth, ipc::MaxFrameHeight));
  }
};
QTEST_GUILESS_MAIN(ChannelsTest)
#include "channels_test.moc"

// A stand-in for oldipmi-core in the desktop's tests (desktop/session_test.cpp):
// it plays a script against the control (fd 3) and video (fd 4) sockets that a
// Session hands its core, and records every command it receives.
//
// OLDIPMI_FAKE_SCRIPT names the script, a JSON array of steps:
//   {"expect": "op"}     read commands until one with this op
//   {"reply": {...}}     send this event with the id of the last expected command
//   {"send": {...}}      send this event as it is
//   {"raw": "hex"}       send these bytes on the control socket
//   {"frame": [w, h]}    send a w×h picture on the video socket
//   {"videoRaw": "hex"}  send these bytes on the video socket
//   {"stderr": "text"}   write the text to stderr
//   {"exit": code}       end at once with this exit code
//   {"signal": n}        end at once, killed by signal n
// After the last step it records commands until a disconnect or until the
// control socket closes, then ends with 0, as the real core does. Like the real
// core it also ends with 0 on SIGTERM, which Session sends to stop it.
// OLDIPMI_FAKE_LOG names the file for the commands, one JSON object per line.
#include <QByteArray>
#include <QFile>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QtEndian>
#include <csignal>
#include <cstdio>
#include <unistd.h>

static bool readFully(int fd, char *p, size_t n) {
  while (n > 0) {
    ssize_t r = ::read(fd, p, n);
    if (r <= 0)
      return false;
    p += r;
    n -= size_t(r);
  }
  return true;
}

static bool writeFully(int fd, const QByteArray &b) {
  const char *p = b.constData();
  size_t n = size_t(b.size());
  while (n > 0) {
    ssize_t w = ::write(fd, p, n);
    if (w <= 0)
      return false;
    p += w;
    n -= size_t(w);
  }
  return true;
}

static QByteArray framed(const QByteArray &body) {
  QByteArray h(4, 0);
  qToBigEndian<quint32>(quint32(body.size()), h.data());
  return h + body;
}

int main() {
  std::signal(SIGTERM, [](int) { _exit(0); });
  QFile script(QString::fromLocal8Bit(qgetenv("OLDIPMI_FAKE_SCRIPT")));
  QFile log(QString::fromLocal8Bit(qgetenv("OLDIPMI_FAKE_LOG")));
  if (!script.open(QIODevice::ReadOnly) || !log.open(QIODevice::Append)) {
    std::fprintf(stderr, "fakecore: OLDIPMI_FAKE_SCRIPT and OLDIPMI_FAKE_LOG must name files\n");
    return 2;
  }
  const QJsonArray steps = QJsonDocument::fromJson(script.readAll()).array();
  int lastId = 0;
  // Reads one command and records it; false once the control socket closed.
  auto next = [&log](QJsonObject *c) {
    char h[4];
    if (!readFully(3, h, 4))
      return false;
    QByteArray b(int(qFromBigEndian<quint32>(h)), 0);
    if (!readFully(3, b.data(), size_t(b.size())))
      return false;
    *c = QJsonDocument::fromJson(b).object();
    log.write(QJsonDocument(*c).toJson(QJsonDocument::Compact) + "\n");
    log.flush();
    return true;
  };
  auto send = [](const QJsonObject &e) {
    return writeFully(3, framed(QJsonDocument(e).toJson(QJsonDocument::Compact)));
  };
  for (const auto &v : steps) {
    const QJsonObject s = v.toObject();
    bool ok = true;
    if (s.contains("expect")) {
      QJsonObject c;
      do {
        if (!next(&c))
          return 1;
      } while (c["op"] != s["expect"]);
      lastId = c["id"].toInt();
    } else if (s.contains("reply")) {
      QJsonObject e = s["reply"].toObject();
      e["id"] = lastId;
      ok = send(e);
    } else if (s.contains("send")) {
      ok = send(s["send"].toObject());
    } else if (s.contains("raw")) {
      ok = writeFully(3, QByteArray::fromHex(s["raw"].toString().toLatin1()));
    } else if (s.contains("frame")) {
      const quint32 w = quint32(s["frame"][0].toInt()), h = quint32(s["frame"][1].toInt());
      QByteArray b(12, 0);
      qToBigEndian<quint32>(8 + w * h * 4, b.data());
      qToBigEndian<quint32>(w, b.data() + 4);
      qToBigEndian<quint32>(h, b.data() + 8);
      for (quint32 i = 0; i < w * h; ++i) {
        const char pixel[4] = {char(i % w * 255 / w), char(i / w * 255 / h), char(128), char(255)};
        b.append(pixel, 4);
      }
      ok = writeFully(4, b);
    } else if (s.contains("videoRaw")) {
      ok = writeFully(4, QByteArray::fromHex(s["videoRaw"].toString().toLatin1()));
    } else if (s.contains("stderr")) {
      std::fputs(s["stderr"].toString().toLocal8Bit().constData(), stderr);
      std::fflush(stderr);
    } else if (s.contains("exit")) {
      return s["exit"].toInt();
    } else if (s.contains("signal")) {
      std::signal(s["signal"].toInt(), SIG_DFL);
      std::raise(s["signal"].toInt());
    }
    if (!ok)
      return 1;
  }
  for (QJsonObject c; next(&c);)
    if (c["op"] == "disconnect")
      break;
  return 0;
}

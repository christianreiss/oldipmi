#include "devtools/probeui.h"
#include "app/exitcode.h"
#include "controller.h"
#include <QApplication>
#include <QFile>
#include <QImage>
#include <QJsonDocument>
#include <QJsonObject>
#include <QQmlApplicationEngine>
#include <QQuickWindow>
#include <QTimer>
#include <cstdio>

namespace devtools {
bool startProbeUi(QApplication &app, QQmlApplicationEngine &engine, Controller &controller) {
  QFile input;
  if (!input.open(stdin, QIODevice::ReadOnly))
    return false;
  auto json = QJsonDocument::fromJson(input.readAll()).object();
  auto profile = json["profile"].toObject().toVariantMap();
  if (profile.value("id").toString().isEmpty()) {
    for (const auto &v : controller.hosts()) {
      auto saved = v.toMap();
      if (saved.value("host") == profile.value("host")) {
        for (auto it = profile.cbegin(); it != profile.cend(); ++it)
          saved[it.key()] = it.value();
        profile = saved;
        break;
      }
    }
  }
  QObject::connect(&controller, &Controller::changed, &app, [&app, &controller] {
    // Probe diagnostics use the same allowlisted metadata as the GUI.
    QJsonObject event{{"state", controller.state()},
                      {"message", controller.message()},
                      {"info", QJsonObject::fromVariantMap(controller.consoleInfo())}};
    auto bytes = QJsonDocument(event).toJson(QJsonDocument::Compact);
    fprintf(stdout, "%s\n", bytes.constData());
    fflush(stdout);
    if (controller.state() == ipc::StateDisconnected && controller.message().contains(" failed:")) {
      fprintf(stderr, "%s\n", qPrintable(controller.message()));
      app.exit(code(ExitCode::ProbeFailed));
    }
  });
  QTimer::singleShot(0, &controller, [&engine, &controller, json, profile] {
    QMetaObject::invokeMethod(
        engine.rootObjects().first(), "loadHost",
        Q_ARG(QVariant, QVariant(profile)));
    auto details = engine.rootObjects().first()->findChild<QObject *>("consoleDetails");
    if (details) details->setProperty("checked", true);
    controller.connectHost(profile,
                           json["password"].toString());
  });
  // Without a password there is nothing to probe: a prompt would wait for the user.
  QObject::connect(&controller, &Controller::passwordRequired, &app, [&app, &controller] {
    fprintf(stderr, "%s\n", qPrintable(controller.message()));
    app.exit(code(ExitCode::NoPassword));
  });
  QObject::connect(
      &controller, &Controller::frameReady, &app, [&app, &engine, framed = false](QImage im) mutable {
        if (im.isNull() || framed)
          return;
        framed = true;
        QJsonObject size{{"width", im.width()}, {"height", im.height()}};
        fprintf(stdout, "%s\n", QJsonDocument(QJsonObject{{"frame", size}}).toJson(QJsonDocument::Compact).constData());
        fflush(stdout);
        QTimer::singleShot(200, &app, [&app, &engine] {
          auto w = qobject_cast<QQuickWindow *>(engine.rootObjects().first());
          QString path = qEnvironmentVariable("OLDIPMI_TEST_SCREENSHOT");
          if (!path.isEmpty())
            w->grabWindow().save(path);
          app.exit(code(ExitCode::Ok));
        });
      });
  QTimer::singleShot(30000, &app, [&app, &controller] {
    fprintf(stderr, "GUI probe timed out: %s\n",
            qPrintable(controller.message()));
    app.exit(code(ExitCode::ProbeFailed));
  });
  return true;
}
} // namespace devtools

#include "console.h"
#include "controller.h"
#include <QApplication>
#include <QFile>
#include <QJsonDocument>
#include <QJsonObject>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickWindow>
#include <QStandardPaths>
#include <QTimer>
int main(int argc, char **argv) {
  QApplication app(argc, argv);
  app.setApplicationName("OldIPMI");
  app.setOrganizationName("OldIPMI");
  app.setApplicationVersion("0.1.0");
  bool test = app.arguments().contains("--self-test"),
       preview = app.arguments().contains("--preview");
  if (test)
    QStandardPaths::setTestModeEnabled(true);
  qmlRegisterType<Console>("OldIPMI", 1, 0, "ConsoleView");
  Controller controller;
  QQmlApplicationEngine engine;
  engine.rootContext()->setContextProperty("controller", &controller);
  engine.load(QUrl("qrc:/desktop/Main.qml"));
  if (engine.rootObjects().isEmpty())
    return 1;
  auto console = engine.rootObjects().first()->findChild<Console *>("console");
  if (!console)
    return 2;
  QObject::connect(&controller, &Controller::frameReady, console,
                   &Console::setFrame);
  QObject::connect(console, &Console::keyInput, &controller, &Controller::key);
  QObject::connect(console, &Console::pointerInput, &controller,
                   &Controller::pointer);
  QObject::connect(console, &Console::releaseInput, &controller,
                   &Controller::releaseInput);
  if (app.arguments().contains("--probe-ui")) {
    QFile input;
    if (!input.open(stdin, QIODevice::ReadOnly))
      return 4;
    auto json = QJsonDocument::fromJson(input.readAll()).object();
    QTimer::singleShot(0, &controller, [&, json] {
      QMetaObject::invokeMethod(
          engine.rootObjects().first(), "loadHost",
          Q_ARG(QVariant, QVariant(json["profile"].toObject().toVariantMap())));
      controller.connectHost(json["profile"].toObject().toVariantMap(),
                             json["password"].toString());
    });
    QObject::connect(
        &controller, &Controller::frameReady, &app, [&](QImage im) {
          if (im.isNull())
            return;
          QTimer::singleShot(200, &app, [&] {
            auto w = qobject_cast<QQuickWindow *>(engine.rootObjects().first());
            QString path = qEnvironmentVariable("OLDIPMI_TEST_SCREENSHOT");
            if (!path.isEmpty())
              w->grabWindow().save(path);
            app.exit(0);
          });
        });
    QTimer::singleShot(30000, &app, [&] {
      fprintf(stderr, "GUI probe timed out: %s\n",
              qPrintable(controller.message()));
      app.exit(4);
    });
  }
  if (test || preview)
    QTimer::singleShot(100, &controller, &Controller::preview);
  if (test)
    QTimer::singleShot(600, &app, [&] {
      auto w = qobject_cast<QQuickWindow *>(engine.rootObjects().first());
      if (!w || w->grabWindow().isNull())
        app.exit(3);
      else {
        QString path = qEnvironmentVariable("OLDIPMI_TEST_SCREENSHOT");
        if (!path.isEmpty())
          w->grabWindow().save(path);
        app.exit(0);
      }
    });
  return app.exec();
}

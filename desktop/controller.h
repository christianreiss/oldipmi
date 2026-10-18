#pragma once
#include <QImage>
#include <QJsonObject>
#include <QLocalSocket>
#include <QObject>
#include <QProcess>
#include <QVariantList>

class Controller : public QObject {
  Q_OBJECT
  Q_PROPERTY(QVariantList hosts READ hosts NOTIFY hostsChanged)
  Q_PROPERTY(QString state READ state NOTIFY changed)
  Q_PROPERTY(QString message READ message NOTIFY changed)
  Q_PROPERTY(bool mounted READ mounted NOTIFY changed)
public:
  explicit Controller(QObject *parent = nullptr);
  ~Controller();
  QVariantList hosts() const { return m_hosts; }
  QString state() const { return m_state; }
  QString message() const { return m_message; }
  bool mounted() const { return m_mounted; }
  Q_INVOKABLE QVariantMap saveHost(QVariantMap profile, QString password,
                                   bool remember);
  Q_INVOKABLE void removeHost(QString id);
  Q_INVOKABLE void connectHost(QVariantMap profile, QString password);
  Q_INVOKABLE void disconnectHost();
  Q_INVOKABLE void key(int usage, bool down);
  Q_INVOKABLE void pointer(int x, int y, int buttons, int wheel);
  Q_INVOKABLE void releaseInput();
  Q_INVOKABLE void mountISO();
  Q_INVOKABLE void ejectISO();
  Q_INVOKABLE void power(QString action);
  Q_INVOKABLE void openJNLP(QVariantMap profile);
  Q_INVOKABLE QString chooseCertificate(bool key);
  void preview();
signals:
  void changed();
  void hostsChanged();
  void frameReady(QImage image);

private:
  QVariantList m_hosts;
  QString m_state = "disconnected",
          m_message = "Choose a saved host or enter a BMC address.";
  bool m_mounted = false;
  QProcess *m_process = nullptr;
  QLocalSocket *m_control = nullptr, *m_video = nullptr;
  QByteArray m_controlBuffer, m_videoBuffer;
  int m_id = 0;
  void start(QVariantMap profile, QString password, QString jnlp = {});
  void command(QJsonObject obj);
  void fail(QString message);
  void readControl();
  void readVideo();
  void persist();
  void stop();
  QString walletPassword(QString id);
  bool walletWrite(QString id, QString password);
};

#pragma once
#include <QString>
#include <memory>

// Injectable so profile tests never open or modify the user's wallet.
class Credentials {
public:
  virtual ~Credentials() = default;
  virtual bool read(const QString &id, QString &password) = 0;
  virtual bool write(const QString &id, const QString &password) = 0;
  virtual bool remove(const QString &id) = 0;
};
std::unique_ptr<Credentials> kdeCredentials();

#pragma once
#include <QHash>
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

// Passwords kept in memory only, for the self-test and its screenshot tour. With
// `available` false it behaves like a wallet that cannot be opened.
class MemoryCredentials final : public Credentials {
public:
  bool available = true;
  bool read(const QString &id, QString &password) override {
    if (!available || !m_values.contains(id))
      return false;
    password = m_values.value(id);
    return true;
  }
  bool write(const QString &id, const QString &password) override {
    if (available)
      m_values.insert(id, password);
    return available;
  }
  bool remove(const QString &id) override {
    if (available)
      m_values.remove(id);
    return available;
  }

private:
  QHash<QString, QString> m_values;
};

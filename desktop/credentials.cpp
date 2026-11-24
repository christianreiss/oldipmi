#include "credentials.h"
#include <KWallet>

namespace {
auto wallet() {
  return std::unique_ptr<KWallet::Wallet>(KWallet::Wallet::openWallet(
      KWallet::Wallet::NetworkWallet(), 0, KWallet::Wallet::Synchronous));
}
class KDECredentials final : public Credentials {
public:
  Entry snapshot(const QString &id, QString &password) override {
    if (KWallet::Wallet::keyDoesNotExist(KWallet::Wallet::NetworkWallet(), "OldIPMI", id))
      return Entry::Missing;
    auto w = wallet();
    if (!w)
      return Entry::Unavailable;
    if (!w->hasFolder("OldIPMI"))
      return Entry::Missing;
    if (!w->setFolder("OldIPMI"))
      return Entry::Unavailable;
    if (!w->hasEntry(id))
      return Entry::Missing;
    return w->readPassword(id, password) == 0 ? Entry::Present : Entry::Unavailable;
  }
  bool read(const QString &id, QString &password) override {
    return snapshot(id, password) == Entry::Present;
  }
  bool write(const QString &id, const QString &password) override {
    auto w = wallet();
    if (!w || (!w->hasFolder("OldIPMI") && !w->createFolder("OldIPMI")))
      return false;
    return w->setFolder("OldIPMI") && w->writePassword(id, password) == 0;
  }
  bool remove(const QString &id) override {
    if (KWallet::Wallet::keyDoesNotExist(KWallet::Wallet::NetworkWallet(), "OldIPMI", id))
      return true;
    auto w = wallet();
    if (!w)
      return false;
    if (!w->hasFolder("OldIPMI"))
      return true;
    return w->setFolder("OldIPMI") &&
           (!w->hasEntry(id) || w->removeEntry(id) == 0);
  }
};
}
std::unique_ptr<Credentials> kdeCredentials() {
  return std::make_unique<KDECredentials>();
}

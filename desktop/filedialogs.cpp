#include "filedialogs.h"
#include <QFileDialog>

QString FileDialogs::isoImage() {
  return QFileDialog::getOpenFileName(nullptr, "Mount read-only ISO", {}, "ISO images (*.iso)");
}

QString FileDialogs::clientCertificate(bool key) {
  return QFileDialog::getOpenFileName(nullptr, key ? "Client private key" : "Client certificate", {},
                                      "PEM files (*.pem *.crt *.key)");
}
